# This is ~~hand~~ AI-rolled. Will see if we need something better later.
import zipfile
from dataclasses import dataclass
from pathlib import Path
from xml.etree import ElementTree as ET

NAMESPACES = {
    "container": "urn:oasis:names:tc:opendocument:xmlns:container",
    "opf": "http://www.idpf.org/2007/opf",
    "dc": "http://purl.org/dc/elements/1.1/",
    "calibre": "http://calibre.kovidgoyal.net/2009/metadata",
}


@dataclass(slots=True)
class EpubMetadata:
    title: str | None = None
    authors: list[str] | None = None
    series: str | None = None
    series_index: float | None = None
    cover_path: str | None = None
    description: str | None = None
    publisher: str | None = None
    language: str | None = None
    publication_date: str | None = None


def read_metadata(path: Path) -> EpubMetadata:
    """
    Read metadata from an EPUB file.

    EPUB files are ZIP archives with:
    - META-INF/container.xml pointing to the OPF file
    - OPF file containing Dublin Core metadata
    """
    metadata = EpubMetadata()

    try:
        with zipfile.ZipFile(path, "r") as zf:
            opf_path = _find_opf_path(zf)
            if not opf_path:
                return metadata

            opf_content = zf.read(opf_path).decode("utf-8")
            root = ET.fromstring(opf_content)
            opf_dir = str(Path(opf_path).parent)

            _parse_dc_metadata(root, metadata)
            _parse_calibre_metadata(root, metadata)
            _find_cover(root, metadata, opf_dir)

    except (zipfile.BadZipFile, OSError, ET.ParseError):
        pass

    return metadata


def _find_opf_path(zf: zipfile.ZipFile) -> str | None:
    """Find the OPF file path from container.xml."""
    try:
        container = zf.read("META-INF/container.xml").decode("utf-8")
        root = ET.fromstring(container)
        rootfile = root.find(".//container:rootfile", NAMESPACES)
        if rootfile is not None:
            return rootfile.get("full-path")
    except (KeyError, ET.ParseError):
        pass

    # Fallback: look for .opf file directly
    for name in zf.namelist():
        if name.endswith(".opf"):
            return name
    return None


def _parse_dc_metadata(root: ET.Element, metadata: EpubMetadata) -> None:
    """Parse Dublin Core metadata from OPF."""
    md = root.find("opf:metadata", NAMESPACES)
    if md is None:
        return

    # Title
    title_el = md.find("dc:title", NAMESPACES)
    if title_el is not None and title_el.text:
        metadata.title = title_el.text.strip()

    # Authors
    authors = md.findall("dc:creator", NAMESPACES)
    if authors:
        metadata.authors = [a.text.strip() for a in authors if a.text]

    # Description
    desc_el = md.find("dc:description", NAMESPACES)
    if desc_el is not None and desc_el.text:
        metadata.description = desc_el.text.strip()

    # Publisher
    pub_el = md.find("dc:publisher", NAMESPACES)
    if pub_el is not None and pub_el.text:
        metadata.publisher = pub_el.text.strip()

    # Language
    lang_el = md.find("dc:language", NAMESPACES)
    if lang_el is not None and lang_el.text:
        metadata.language = lang_el.text.strip()

    # Publication date
    date_el = md.find("dc:date", NAMESPACES)
    if date_el is not None and date_el.text:
        metadata.publication_date = date_el.text.strip()

    # EPUB3 series info (belongs-to-collection)
    for meta in md.findall("opf:meta", NAMESPACES):
        prop = meta.get("property", "")
        if prop == "belongs-to-collection" and meta.text:
            metadata.series = meta.text.strip()
        elif prop == "group-position" and meta.text:
            try:
                metadata.series_index = float(meta.text.strip())
            except ValueError:
                pass


def _parse_calibre_metadata(root: ET.Element, metadata: EpubMetadata) -> None:
    """Parse Calibre-specific metadata (series info)."""
    md = root.find("opf:metadata", NAMESPACES)
    if md is None:
        return

    for meta in md.findall("opf:meta", NAMESPACES):
        name = meta.get("name", "")
        content = meta.get("content", "")

        if name == "calibre:series" and content and not metadata.series:
            metadata.series = content.strip()
        elif name == "calibre:series_index" and content and metadata.series_index is None:
            try:
                metadata.series_index = float(content.strip())
            except ValueError:
                pass


def _find_cover(root: ET.Element, metadata: EpubMetadata, opf_dir: str) -> None:
    """Find the cover image path."""
    md = root.find("opf:metadata", NAMESPACES)
    manifest = root.find("opf:manifest", NAMESPACES)
    if md is None or manifest is None:
        return

    # Method 1: Look for meta name="cover" pointing to manifest item
    cover_id = None
    for meta in md.findall("opf:meta", NAMESPACES):
        if meta.get("name") == "cover":
            cover_id = meta.get("content")
            break

    if cover_id:
        item = manifest.find(f"opf:item[@id='{cover_id}']", NAMESPACES)
        if item is not None:
            href = item.get("href")
            if href:
                metadata.cover_path = _resolve_path(opf_dir, href)
                return

    # Method 2: Look for item with properties="cover-image"
    for item in manifest.findall("opf:item", NAMESPACES):
        if "cover-image" in (item.get("properties") or ""):
            href = item.get("href")
            if href:
                metadata.cover_path = _resolve_path(opf_dir, href)
                return

    # Method 3: Look for item with id containing "cover" and image media type
    for item in manifest.findall("opf:item", NAMESPACES):
        item_id = (item.get("id") or "").lower()
        media_type = item.get("media-type") or ""
        if "cover" in item_id and media_type.startswith("image/"):
            href = item.get("href")
            if href:
                metadata.cover_path = _resolve_path(opf_dir, href)
                return


def _resolve_path(opf_dir: str, href: str) -> str:
    """Resolve a relative path from the OPF directory."""
    if opf_dir == ".":
        return href
    return f"{opf_dir}/{href}"
