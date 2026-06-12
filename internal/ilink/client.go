package ilink

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/realli07kkk/webot-msg/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const DefaultBaseURL = "https://ilinkai.weixin.qq.com"

type Client struct {
	BaseURL   string
	transport http.RoundTripper
}

type MessageItem struct {
	Type     int `json:"type"`
	TextItem struct {
		Text string `json:"text"`
	} `json:"text_item"`
}

type WeixinMessage struct {
	FromUserID   string        `json:"from_user_id"`
	ContextToken string        `json:"context_token"`
	ItemList     []MessageItem `json:"item_list"`
}

type UpdatesResponse struct {
	Ret                  int             `json:"ret"`
	Errcode              int             `json:"errcode"`
	GetUpdatesBuf        string          `json:"get_updates_buf"`
	LongpollingTimeoutMs int             `json:"longpolling_timeout_ms"`
	Msgs                 []WeixinMessage `json:"msgs"`
}

func NewClient(baseURL string) *Client {
	return NewClientWithTransport(baseURL, instrumentedTransport(http.DefaultTransport))
}

func NewClientWithTransport(baseURL string, transport http.RoundTripper) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if transport == nil {
		transport = instrumentedTransport(http.DefaultTransport)
	}
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		transport: transport,
	}
}

func (c *Client) QRLogin() (*config.UserConfig, error) {
	return c.QRLoginWithWriter(os.Stdout)
}

func (c *Client) QRLoginWithWriter(out io.Writer) (*config.UserConfig, error) {
	httpClient := c.httpClient(10 * time.Second)

outer:
	for {
		resp, err := httpClient.Get(c.endpoint("/ilink/bot/get_bot_qrcode?bot_type=3"))
		if err != nil {
			return nil, err
		}

		var qrRes struct {
			QRcode           string `json:"qrcode"`
			QRcodeImgContent string `json:"qrcode_img_content"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&qrRes); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		qrterminal.GenerateHalfBlock(qrRes.QRcodeImgContent, qrterminal.L, out)
		fmt.Fprintln(out, "Please scan the QR code to log in")

		for {
			statusReq, err := http.NewRequest("GET", c.endpoint("/ilink/bot/get_qrcode_status?qrcode="+url.QueryEscape(qrRes.QRcode)), nil)
			if err != nil {
				return nil, err
			}
			commonHeaders(statusReq, false, "")
			statusReq.Header.Set("iLink-App-ClientVersion", "1")

			statusClient := c.httpClient(35 * time.Second)
			statusResp, err := statusClient.Do(statusReq)
			if err != nil {
				continue
			}

			var statusRes struct {
				Status      string `json:"status"`
				BotToken    string `json:"bot_token"`
				IlinkBotID  string `json:"ilink_bot_id"`
				IlinkUserID string `json:"ilink_user_id"`
			}
			decodeErr := json.NewDecoder(statusResp.Body).Decode(&statusRes)
			statusResp.Body.Close()
			if decodeErr != nil {
				return nil, decodeErr
			}

			switch statusRes.Status {
			case "wait":
			case "scaned":
				fmt.Fprintln(out, "Scanned, please confirm on your phone...")
			case "expired":
				fmt.Fprintln(out, "QR code expired, refreshing...")
				continue outer
			case "confirmed":
				fmt.Fprintln(out, "Login confirmed! BotID:", statusRes.IlinkBotID)
				apiToken, err := config.GenerateToken(16)
				if err != nil {
					return nil, err
				}
				return &config.UserConfig{
					BotToken:    statusRes.BotToken,
					BotID:       statusRes.IlinkBotID,
					IlinkUserID: statusRes.IlinkUserID,
					APIToken:    apiToken,
				}, nil
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func (c *Client) GetUpdates(user config.UserConfig, timeout time.Duration) (*UpdatesResponse, error) {
	reqData := map[string]interface{}{
		"get_updates_buf": user.GetUpdatesBuf,
		"base_info": map[string]string{
			"channel_version": "1.0.0",
		},
	}
	body, err := json.Marshal(reqData)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.endpoint("/ilink/bot/getupdates"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	commonHeaders(req, true, user.BotToken)

	httpClient := c.httpClient(timeout)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var updateRes UpdatesResponse
	if err := json.Unmarshal(bodyBytes, &updateRes); err != nil {
		return nil, err
	}
	if updateRes.Ret != 0 || updateRes.Errcode != 0 {
		return nil, fmt.Errorf("getupdates ret=%d errcode=%d", updateRes.Ret, updateRes.Errcode)
	}
	return &updateRes, nil
}

func (c *Client) SendMessage(user config.UserConfig, to string, text string, contextToken string) error {
	return c.SendMessageContext(context.Background(), user, to, text, contextToken)
}

func (c *Client) SendMessageContext(ctx context.Context, user config.UserConfig, to string, text string, contextToken string) error {
	reqData := map[string]interface{}{
		"msg": map[string]interface{}{
			"from_user_id":  "",
			"to_user_id":    to,
			"client_id":     fmt.Sprintf("openclaw-weixin:%d-%s", time.Now().UnixMilli(), randomHex(4)),
			"message_type":  2,
			"message_state": 2,
			"context_token": contextToken,
			"item_list": []map[string]interface{}{
				{
					"type": 1,
					"text_item": map[string]string{
						"text": text,
					},
				},
			},
		},
		"base_info": map[string]string{
			"channel_version": "1.0.2",
		},
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint("/ilink/bot/sendmessage"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	commonHeaders(req, true, user.BotToken)

	httpClient := c.httpClient(10 * time.Second)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var res struct {
		Ret     int    `json:"ret"`
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
		ErrMsg  string `json:"err_msg"`
	}
	_ = json.Unmarshal(bodyBytes, &res)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if res.Ret != 0 || res.Errcode != 0 {
		msg := res.Errmsg
		if msg == "" {
			msg = res.ErrMsg
		}
		if msg == "" {
			msg = string(bodyBytes)
		}
		return fmt.Errorf("API Error: ret=%d, errcode=%d, msg=%s", res.Ret, res.Errcode, msg)
	}
	return nil
}

func (c *Client) SendTyping(user config.UserConfig, status int) error {
	return c.SendTypingContext(context.Background(), user, status)
}

func (c *Client) SendTypingContext(ctx context.Context, user config.UserConfig, status int) error {
	ticket, err := c.GetBotConfigContext(ctx, user)
	if err != nil {
		return err
	}

	reqData := map[string]interface{}{
		"ilink_user_id": user.IlinkUserID,
		"typing_ticket": ticket,
		"status":        status,
		"base_info": map[string]string{
			"channel_version": "1.0.0",
		},
	}
	body, err := json.Marshal(reqData)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint("/ilink/bot/sendtyping"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	commonHeaders(req, true, user.BotToken)

	httpClient := c.httpClient(10 * time.Second)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Ret int `json:"ret"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return err
	}
	if res.Ret != 0 {
		return fmt.Errorf("sendtyping ret %d", res.Ret)
	}
	return nil
}

func (c *Client) GetBotConfig(user config.UserConfig) (string, error) {
	return c.GetBotConfigContext(context.Background(), user)
}

func (c *Client) GetBotConfigContext(ctx context.Context, user config.UserConfig) (string, error) {
	reqData := map[string]interface{}{
		"ilink_user_id": user.IlinkUserID,
		"context_token": user.ContextToken,
		"base_info": map[string]string{
			"channel_version": "1.0.0",
		},
	}
	body, err := json.Marshal(reqData)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint("/ilink/bot/getconfig"), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	commonHeaders(req, true, user.BotToken)

	httpClient := c.httpClient(10 * time.Second)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Ret          int    `json:"ret"`
		TypingTicket string `json:"typing_ticket"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return "", err
	}
	if res.Ret != 0 {
		return "", fmt.Errorf("getconfig ret %d", res.Ret)
	}
	return res.TypingTicket, nil
}

func (c *Client) endpoint(path string) string {
	return c.BaseURL + path
}

func (c *Client) httpClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: c.transport,
	}
}

func instrumentedTransport(base http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(base, otelhttp.WithFilter(func(req *http.Request) bool {
		return oteltrace.SpanContextFromContext(req.Context()).IsValid()
	}))
}

func commonHeaders(req *http.Request, isJSON bool, token string) {
	if isJSON {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("X-WECHAT-UIN", randomWechatUin())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func randomWechatUin() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return base64.StdEncoding.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
	}
	val := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(val), 10)))
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}
