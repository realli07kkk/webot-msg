#!/usr/bin/env python3
import argparse
import json
import re
import sys
import urllib.request
from datetime import datetime, timezone
from html.parser import HTMLParser
from urllib.parse import urljoin


HN_URL = "https://news.ycombinator.com/"


class HNFrontPageParser(HTMLParser):
    def __init__(self, base_url):
        super().__init__(convert_charrefs=True)
        self.base_url = base_url
        self.items = []
        self.current = None
        self.in_rank = False
        self.in_titleline = False
        self.in_title_link = False
        self.in_score = False
        self.in_subtext = False
        self.title_parts = []
        self.subtext_item = None

    def handle_starttag(self, tag, attrs):
        attrs_dict = dict(attrs)
        classes = set(attrs_dict.get("class", "").split())

        if tag == "tr" and "athing" in classes:
            self.current = {
                "rank": None,
                "title": "",
                "url": "",
                "hn_id": attrs_dict.get("id", ""),
                "hn_url": "",
                "score": None,
                "comments": None,
            }
            if self.current["hn_id"]:
                self.current["hn_url"] = urljoin(self.base_url, f"item?id={self.current['hn_id']}")
            self.items.append(self.current)

        if tag == "span" and "rank" in classes:
            self.in_rank = True
        elif tag == "span" and "titleline" in classes:
            self.in_titleline = True
        elif tag == "span" and "score" in classes:
            self.in_score = True
            self.subtext_item = self.items[-1] if self.items else None

        if tag == "td" and "subtext" in classes:
            self.in_subtext = True
            self.subtext_item = self.items[-1] if self.items else None

        if (
            tag == "a"
            and self.in_titleline
            and self.current is not None
            and not self.current["url"]
        ):
            self.current["url"] = urljoin(self.base_url, attrs_dict.get("href", ""))
            self.in_title_link = True
            self.title_parts = []

    def handle_endtag(self, tag):
        if tag == "span":
            self.in_rank = False
            self.in_score = False
            self.in_titleline = False
        elif tag == "td":
            self.in_subtext = False
            self.subtext_item = None
        elif tag == "a" and self.in_title_link:
            if self.current is not None:
                self.current["title"] = " ".join("".join(self.title_parts).split())
            self.in_title_link = False
            self.title_parts = []

    def handle_data(self, data):
        text = data.strip()
        if not text:
            return

        if self.in_rank and self.current is not None:
            match = re.search(r"\d+", text)
            if match:
                self.current["rank"] = int(match.group(0))
            return

        if self.in_title_link:
            self.title_parts.append(data)
            return

        if self.in_score and self.subtext_item is not None:
            match = re.search(r"\d+", text)
            if match:
                self.subtext_item["score"] = int(match.group(0))
            return

        if self.in_subtext and self.subtext_item is not None:
            if text == "discuss":
                self.subtext_item["comments"] = 0
            else:
                match = re.search(r"(\d+)\s+comments?", text)
                if match:
                    self.subtext_item["comments"] = int(match.group(1))


def fetch(url):
    request = urllib.request.Request(
        url,
        headers={
            "User-Agent": "Mozilla/5.0 hacker-news-brief skill",
        },
    )
    with urllib.request.urlopen(request, timeout=20) as response:
        return response.read().decode("utf-8", errors="replace")


def main():
    parser = argparse.ArgumentParser(description="Fetch Hacker News front page stories as JSON.")
    parser.add_argument("--url", default=HN_URL, help="HN page URL to fetch")
    parser.add_argument("--limit", type=int, default=30, help="Number of stories to output")
    args = parser.parse_args()

    html = fetch(args.url)
    hn_parser = HNFrontPageParser(args.url)
    hn_parser.feed(html)

    stories = [
        item
        for item in hn_parser.items
        if item.get("rank") is not None and item.get("title") and item.get("url")
    ][: args.limit]

    payload = {
        "source_url": args.url,
        "fetched_at": datetime.now(timezone.utc).isoformat(),
        "count": len(stories),
        "stories": stories,
    }
    json.dump(payload, sys.stdout, ensure_ascii=False, indent=2)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
