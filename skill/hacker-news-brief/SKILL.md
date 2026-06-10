---
name: hacker-news-brief
description: Generate Simplified Chinese Hacker News morning briefs from the current HN front page. Use when an agent needs to fetch news.ycombinator.com in a CLI-only Linux environment, collect the first 30 stories in page order, use Exa MCP first to read accessible original links, synthesize 3-5 overall trends, and produce a polished Chinese briefing with source-access caveats.
---

# Hacker News Brief

Generate a Simplified Chinese morning brief from the current Hacker News front page. The output should feel like an editorial briefing, not a raw feed dump.

This skill is designed for CLI-only Linux hosts. Use Exa MCP when available, HTTP fetches, command-line tools, and the bundled Python script; do not require a browser, Chrome extension, or desktop UI.

## Linux CLI Requirements

- Required: `python3` with the standard library.
- Preferred when available: Exa MCP, especially `web_fetch_exa`.
- Useful fallback tools: `curl` or `wget` for manual source checks.
- Network access to `https://news.ycombinator.com/` and original story URLs is required.
- Do not use GUI browser automation, screenshots, clipboard tools, or macOS-only commands.

## Workflow

1. Fetch `https://news.ycombinator.com/` and collect the first 30 stories in page order.
   - If Exa MCP is available, try `web_fetch_exa` for the HN front page first.
   - Accept the Exa result only if it preserves enough structure to recover exactly the first 30 stories in HN page order with English titles and original URLs.
   - If Exa output is ambiguous, incomplete, reordered, or missing original URLs, immediately fall back to running `scripts/fetch_hn_frontpage.py --limit 30` from this skill folder.
   - Preserve HN page order exactly. Do not reorder by score, comments, domain, or perceived importance.
   - Preserve each story's English title exactly as shown on HN.

2. Fetch/read the original link for each story when technically possible.
   - Use Exa MCP `web_fetch_exa` first for original links. Batch multiple URLs when practical.
   - Treat an Exa result as usable only when it returns enough readable content to support a factual 1-2 sentence summary.
   - If Exa fails, returns empty/irrelevant content, times out, or cannot access the source, fall back to CLI `curl`/`wget`, then to HN item page context, then to HN title and visible metadata.
   - Base summaries on accessible original content, not only the HN title.
   - Use HN title and visible HN metadata only when the original is unavailable, blocked, rate-limited, JS-only, a PDF that cannot be read, a GitHub page that cannot be fetched, or an X/Twitter post that cannot be accessed.
   - If the link is an HN item page, summarize from the HN-visible Launch/Ask/Show context and label the limitation.

3. Write 3-5 overall trends before the story list.
   - Trends must synthesize multiple stories into broader signals.
   - Use editorial labels, for example: `AI 能力跃迁与信任问题并行`, `开发者工具转向安全默认`.
   - Explain why each trend matters in 1-3 sentences.

4. List all 30 stories.
   - For each item include: sequence number, English title, original link, and 1-2 Chinese summary sentences.
   - If source access failed, include a clear caveat in the summary, for example: `摘要仅基于 Hacker News 标题和页面可见信息。`
   - Do not include HN points or comment counts unless the user explicitly asks for heat metrics.

5. End with a short generation note.
   - State that summaries are based on accessible original pages.
   - State that inaccessible sources were marked explicitly.
   - Do not claim every original was read unless that is true.

## Output Format

Use this structure:

```text
☕ Hacker News 晨间简报 — YYYY 年 M 月 D 日

📊 今日趋势
1. {趋势标题} {1-3 句综合分析}
2. {趋势标题} {1-3 句综合分析}
3. {趋势标题} {1-3 句综合分析}

📋 今日 Top 30 新闻
1. {English title}
🔗 {original_url}
{1-2 句中文摘要；必要时说明访问限制。}

2. {English title}
🔗 {original_url}
{1-2 句中文摘要；必要时说明访问限制。}

...

🤖 本简报由 AI 自动生成。摘要基于可访问的原文内容；无法访问的原文已明确标注为基于 Hacker News 标题和页面可见信息。
```

## Style Rules

- Default to Simplified Chinese.
- Keep English titles unchanged.
- Keep URLs exact and visible.
- Prefer concrete product names, organizations, people, legal consequences, API changes, version numbers, and developer impact.
- Use cautious language for inference: `可能`, `显示`, `指向`, `引发讨论`.
- Avoid duplicate top-level sections such as both `今日趋势` and `整体趋势`.
- Avoid terse score-board summaries like `标题 — 链接 — 热度 N 分`.
- Avoid overclaiming when only title metadata was available.
- Keep each story compact; the brief can be long, but each item should remain scannable.

## Source Access Labels

Use precise labels inside the relevant story summary:

- Original read successfully: no caveat needed.
- Original unreachable, blocked, or timed out: `摘要仅基于 Hacker News 标题和页面可见信息。`
- X/Twitter inaccessible: `原文为 X/Twitter 帖子，摘要基于 Hacker News 标题和页面可见信息。`
- PDF inaccessible or not parsed: `原文为 PDF，未能完整解析；摘要基于标题和可见信息。`
- GitHub inaccessible or rate-limited: `GitHub 页面未能完整访问；摘要基于 HN 标题及页面可见信息。`
- JS-heavy page inaccessible: `原文页面依赖 JavaScript 或访问受限；摘要基于可见信息。`

## Fetch Priority

Use this order for each original story URL:

1. Exa MCP `web_fetch_exa`.
2. Exa MCP `web_search_exa` only when direct fetch fails and a searchable article title/domain can identify the same source.
3. CLI HTTP fetch with `curl` or `wget`.
4. HN item page content and comments context for `Launch HN`, `Ask HN`, `Show HN`, or unavailable original links.
5. HN title and visible page metadata only.

Consider Exa failed when the tool is unavailable, raises an error, returns no content, returns unrelated content, strips the substance needed for summarization, or cannot distinguish the requested URL from another page. Always label summaries produced from fallback levels 4-5.

## Script

Run:

```bash
python3 <skill-dir>/scripts/fetch_hn_frontpage.py --limit 30
```

The script outputs JSON with `rank`, `title`, `url`, `hn_url`, `score`, and `comments`. Use `score` and `comments` only as optional context for trend judgment; do not print them by default.

If the script fails, fall back to fetching `https://news.ycombinator.com/` with a CLI HTTP client and extracting the first 30 story rows manually.
