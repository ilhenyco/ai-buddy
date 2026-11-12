import hashlib
import re
import unicodedata

from .models import Chunk, Document


def chunk_document(doc: Document, max_chars: int = 1200, overlap: int = 150) -> list[Chunk]:
    """Keep paragraph/list/table/code structure and never overlap across headings."""
    text = unicodedata.normalize("NFC", doc.text).replace("\r\n", "\n")
    sections: list[tuple[str, list[str]]] = []
    headings: list[tuple[int, str]] = []
    current: list[str] = []
    path = doc.title
    fenced = False
    fence_char = ""
    for line in text.splitlines():
        fence = re.match(r"^\s*(`{3,}|~{3,})", line)
        if fence:
            if not fenced:
                fenced, fence_char = True, fence[1][0]
            elif fence[1][0] == fence_char:
                fenced = False
        heading = re.match(r"^(#{1,6})\s+(.+?)\s*#*\s*$", line) if not fenced else None
        if heading:
            if current:
                sections.append((path, current))
            level = len(heading[1])
            headings = [(n, title) for n, title in headings if n < level]
            headings.append((level, heading[2]))
            path = " / ".join(title for _, title in headings)
            current = []
        else:
            current.append(line)
    sections.append((path, current))
    result: list[Chunk] = []
    for section, lines in sections:
        body = "\n".join(lines).strip()
        while body:
            end = min(len(body), max_chars)
            if end < len(body):
                candidates = [body.rfind(s, max_chars // 2, end) for s in ("\n\n", "\n", ". ", " ")]
                boundary = next((pos for pos in candidates if pos > 0), -1)
                if boundary > 0:
                    end = boundary
            part = body[:end].strip()
            ordinal = len(result) + 1
            suffix = hashlib.sha256(f"{doc.revision}:{ordinal}:{part}".encode()).hexdigest()[:16]
            result.append(
                Chunk(
                    chunkId=f"{doc.documentId}-{suffix}",
                    section=section,
                    text=part,
                    ordinal=ordinal,
                )
            )
            if end == len(body):
                break
            # Overlap at a whitespace boundary; always make forward progress.
            start = max(end - overlap, 1)
            space = body.find(" ", start, end)
            if space >= 0:
                start = space + 1
            body = body[start:].strip()
    return result
