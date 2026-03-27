#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import re
from pathlib import Path
from typing import Any


ENCODING_CANDIDATES = ("cp936", "gb18030", "cp1252", "latin1")
MOJIBAKE_HINT_RE = re.compile(r"[\u0400-\u04ff\u20ac\u201a-\u203a\u9225\u9353\u93b5\u93c2\u93ba\u941c\u9428\u95ab\u6d93\u6769]")


def suspicious_score(text: str) -> int:
    score = 0
    for ch in text:
        o = ord(ch)
        if 0x0400 <= o <= 0x04FF:
            score += 4
        elif 0x4E00 <= o <= 0x9FFF:
            if ch in "鍓闇鈥鏍埛鍙闃鍥":
                score += 4
        elif 0x80 <= o <= 0x024F:
            score += 1
        elif 0x2000 <= o <= 0x20FF:
            score += 2
    if "??" in text:
        score += 2
    if MOJIBAKE_HINT_RE.search(text):
        score += 4
    return score


def cjk_score(text: str) -> int:
    return sum(1 for ch in text if 0x4E00 <= ord(ch) <= 0x9FFF)


def try_redecode_once(text: str, encoding: str) -> str | None:
    try:
        return text.encode(encoding).decode("utf-8")
    except Exception:
        return None


def pick_best_variant(original: str, variants: list[str]) -> str:
    best = original
    best_score = cjk_score(original) * 4 - suspicious_score(original)
    for candidate in variants:
        candidate_score = cjk_score(candidate) * 4 - suspicious_score(candidate)
        if candidate_score > best_score:
            best = candidate
            best_score = candidate_score
    return best


def repair_text(text: str) -> str:
    seen = {text}
    frontier = [text]
    variants: list[str] = [text]

    for _ in range(2):
        new_frontier: list[str] = []
        for current in frontier:
            for encoding in ENCODING_CANDIDATES:
                repaired = try_redecode_once(current, encoding)
                if repaired and repaired not in seen:
                    seen.add(repaired)
                    variants.append(repaired)
                    new_frontier.append(repaired)
        if not new_frontier:
            break
        frontier = new_frontier

    return pick_best_variant(text, variants)


def walk_and_repair(value: Any, stats: dict[str, int]) -> Any:
    if isinstance(value, str):
        repaired = repair_text(value)
        if repaired != value:
            stats["changed_strings"] += 1
        if suspicious_score(repaired) > 0:
            stats["remaining_suspicious_strings"] += 1
        return repaired
    if isinstance(value, list):
        return [walk_and_repair(item, stats) for item in value]
    if isinstance(value, dict):
        return {key: walk_and_repair(val, stats) for key, val in value.items()}
    return value


def relative_session_path(session_path: Path) -> Path:
    parts = session_path.parts
    if "sessions" in parts:
        idx = parts.index("sessions")
        return Path(*parts[idx + 1 :])
    return Path(session_path.name)


def process_file(src: Path, dst: Path) -> dict[str, int]:
    stats = {
        "lines": 0,
        "changed_lines": 0,
        "changed_strings": 0,
        "remaining_suspicious_strings": 0,
    }
    dst.parent.mkdir(parents=True, exist_ok=True)
    out_lines: list[str] = []

    for raw_line in src.read_text(encoding="utf-8").splitlines():
        stats["lines"] += 1
        try:
            payload = json.loads(raw_line)
        except Exception:
            out_lines.append(raw_line)
            continue

        per_line_stats = {"changed_strings": 0, "remaining_suspicious_strings": 0}
        repaired = walk_and_repair(payload, per_line_stats)
        if per_line_stats["changed_strings"] > 0:
            stats["changed_lines"] += 1
        stats["changed_strings"] += per_line_stats["changed_strings"]
        stats["remaining_suspicious_strings"] += per_line_stats["remaining_suspicious_strings"]
        out_lines.append(json.dumps(repaired, ensure_ascii=False, separators=(",", ":")))

    dst.write_text("\n".join(out_lines) + "\n", encoding="utf-8")
    return stats


def main() -> None:
    parser = argparse.ArgumentParser(description="Recover mojibake in local Codex session JSONL files.")
    parser.add_argument("sessions", nargs="+", help="One or more session JSONL files to repair.")
    parser.add_argument("--output-root", required=True, help="Directory for repaired copies.")
    args = parser.parse_args()

    output_root = Path(args.output_root)
    summaries = []

    for session in args.sessions:
        src = Path(session)
        dst = output_root / relative_session_path(src)
        stats = process_file(src, dst)
        summaries.append(
            {
                "source": str(src),
                "output": str(dst),
                **stats,
            }
        )

    print(json.dumps(summaries, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
