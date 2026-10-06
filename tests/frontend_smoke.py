#!/usr/bin/env python3
"""Dependency-free source smoke checks; browser interaction is tested separately."""
from html.parser import HTMLParser
from pathlib import Path
import re
import shutil
import subprocess
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
WEB = ROOT / "web"
passed = 0


def check(condition, label):
    global passed
    assert condition, label
    passed += 1
    print(f"[PASS] {label}")


class Document(HTMLParser):
    def __init__(self):
        super().__init__()
        self.elements = []

    def handle_starttag(self, tag, attrs):
        self.elements.append((tag, dict(attrs)))


document = Document()
document.feed((WEB / "index.html").read_text())
ids = [attrs["id"] for _, attrs in document.elements if "id" in attrs]
check(len(ids) == len(set(ids)), "HTML has unique IDs")
check(all(not key.startswith("on") for _, attrs in document.elements for key in attrs),
      "HTML has no inline event handlers")
check(any(tag == "meta" and attrs.get("name") == "viewport" for tag, attrs in document.elements),
      "Responsive viewport declared")
required = {"search-form", "search-input", "search-button", "results", "theme-toggle", "viewer",
            "viewer-prev", "viewer-next", "viewer-play", "viewer-mute", "viewer-download", "viewer-close"}
check(required.issubset(ids), "Search, results and media controls exist")
check(any(tag == "label" and attrs.get("for") == "search-input" for tag, attrs in document.elements),
      "Search has an associated label")
check(any(tag == "dialog" and attrs.get("id") == "viewer" and attrs.get("aria-label")
          for tag, attrs in document.elements), "Media viewer is a labeled native dialog")
assets = set()
for tag, attrs in document.elements:
    for attr in ("src", "href"):
        value = attrs.get(attr, "")
        if value.startswith("/assets/"):
            source, _, fragment = value.partition("#")
            assets.add(WEB / source.lstrip("/"))
            if fragment:
                xml = ET.parse(WEB / source.lstrip("/"))
                assert any(item.get("id") == fragment for item in xml.iter()), value
check(all(asset.is_file() for asset in assets), "Every HTML asset and SVG icon exists")
check(all(tag != "script" or attrs.get("src", "").startswith("/assets/")
          for tag, attrs in document.elements), "Scripts are local CSP-compatible assets")
javascript = (WEB / "assets/js/app.js").read_text()
check(not re.search(r"\b(innerHTML|outerHTML|insertAdjacentHTML|document\.write|eval)\b|new\s+Function\s*\(", javascript),
      "JavaScript has no unsafe HTML or code evaluation sinks")
check("textContent" in javascript and "createElement" in javascript,
      "Dynamic provider content uses text and DOM construction")
check("img.loading = 'lazy'" in javascript, "Gallery images are lazy-loaded")
check("credentials: 'omit'" in javascript, "API fetches omit browser credentials")
check("capabilities" in javascript and "UNAVAILABLE" in javascript,
      "Frontend consumes provider capabilities and unavailable states")
styles = (WEB / "assets/css/app.css").read_text()
check("var(--" in styles and "@media" in styles, "Styles use tokens and responsive breakpoints")
payload = sum(file.stat().st_size for file in WEB.rglob("*")
              if file.is_file() and "fixtures" not in file.parts)
check(payload < 250 * 1024, f"Frontend payload below 250 KB ({payload:,} bytes excluding media)")
node = shutil.which("node")
check(node is not None, "Node available for JavaScript syntax validation")
subprocess.run([node, "--check", str(WEB / "assets/js/app.js")], check=True)
check(True, "JavaScript syntax parses")
print(f"\n{passed} passed\n0 failed")
