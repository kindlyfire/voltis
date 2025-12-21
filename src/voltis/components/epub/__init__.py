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
    "xhtml": "http://www.w3.org/1999/xhtml",
    "ncx": "http://www.daisy.org/z3986/2005/ncx/",
    "epub": "http://www.idpf.org/2007/ops",
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


@dataclass(slots=True)
class EpubChapter:
    id: str
    href: str
    title: str | None = None
    linear: bool = True


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


def list_chapters(path: Path) -> list[EpubChapter]:
    """
    List all chapters in an EPUB file in reading order.

    Returns chapters from the spine with titles extracted from the
    navigation document (EPUB3) or toc.ncx (EPUB2).
    """
    with zipfile.ZipFile(path, "r") as zf:
        opf_path = _find_opf_path(zf)
        if not opf_path:
            raise ValueError("No OPF file found in EPUB")

        opf_content = zf.read(opf_path).decode("utf-8")
        root = ET.fromstring(opf_content)
        opf_dir = str(Path(opf_path).parent)

        manifest = root.find("opf:manifest", NAMESPACES)
        spine = root.find("opf:spine", NAMESPACES)
        if manifest is None or spine is None:
            raise ValueError("Invalid OPF: missing manifest or spine")

        # Build manifest lookup: id -> (href, properties, media-type)
        manifest_items: dict[str, tuple[str, str, str]] = {}
        for item in manifest.findall("opf:item", NAMESPACES):
            item_id = item.get("id")
            href = item.get("href")
            if item_id and href:
                props = item.get("properties") or ""
                media_type = item.get("media-type") or ""
                manifest_items[item_id] = (href, props, media_type)

        # Get chapter titles from navigation
        titles = _parse_nav_titles(zf, manifest_items, opf_dir)

        # Build chapters list from spine
        chapters: list[EpubChapter] = []
        for itemref in spine.findall("opf:itemref", NAMESPACES):
            idref = itemref.get("idref")
            if idref and idref in manifest_items:
                href, _, _ = manifest_items[idref]
                full_href = _resolve_path(opf_dir, href)
                title = titles.get(href) or titles.get(full_href)
                linear = itemref.get("linear", "yes") != "no"
                chapters.append(EpubChapter(id=idref, href=full_href, title=title, linear=linear))

        if chapters and "cover" in (chapters[0].title or chapters[0].id).lower():
            chapters = chapters[1:]

        return chapters


def _parse_nav_titles(
    zf: zipfile.ZipFile,
    manifest_items: dict[str, tuple[str, str, str]],
    opf_dir: str,
) -> dict[str, str]:
    """
    Parse chapter titles from navigation document.

    Tries EPUB3 nav document first, then falls back to EPUB2 toc.ncx.
    Returns a dict mapping href -> title.
    """
    titles: dict[str, str] = {}

    # Try EPUB3 nav document first
    for _, (href, props, media_type) in manifest_items.items():
        if "nav" in props:
            nav_path = _resolve_path(opf_dir, href)
            try:
                nav_content = zf.read(nav_path).decode("utf-8")
                titles = _parse_epub3_nav(nav_content, opf_dir)
                if titles:
                    return titles
            except (KeyError, ET.ParseError):
                pass

    # Fall back to EPUB2 toc.ncx
    for _, (href, props, media_type) in manifest_items.items():
        if media_type == "application/x-dtbncx+xml":
            ncx_path = _resolve_path(opf_dir, href)
            try:
                ncx_content = zf.read(ncx_path).decode("utf-8")
                titles = _parse_ncx_titles(ncx_content, opf_dir)
                if titles:
                    return titles
            except (KeyError, ET.ParseError):
                pass

    return titles


def _parse_epub3_nav(nav_content: str, opf_dir: str) -> dict[str, str]:
    """Parse titles from EPUB3 navigation document."""
    titles: dict[str, str] = {}
    root = ET.fromstring(nav_content)

    # Find the toc nav element
    # Try with epub namespace first, then without
    toc_nav = None
    for nav in root.iter("{http://www.w3.org/1999/xhtml}nav"):
        epub_type = nav.get("{http://www.idpf.org/2007/ops}type") or nav.get("epub:type")
        if epub_type == "toc":
            toc_nav = nav
            break

    if toc_nav is None:
        return titles

    # Parse all anchor elements in the nav
    for a in toc_nav.iter("{http://www.w3.org/1999/xhtml}a"):
        href = a.get("href")
        text = "".join(a.itertext()).strip()
        if href and text:
            # Remove fragment identifier for matching
            base_href = href.split("#")[0]
            if base_href:
                full_href = _resolve_path(opf_dir, base_href)
                titles[base_href] = text
                titles[full_href] = text

    return titles


def _parse_ncx_titles(ncx_content: str, opf_dir: str) -> dict[str, str]:
    """Parse titles from EPUB2 toc.ncx file."""
    titles: dict[str, str] = {}
    root = ET.fromstring(ncx_content)

    # Find all navPoint elements
    for navpoint in root.iter("{http://www.daisy.org/z3986/2005/ncx/}navPoint"):
        # Get the text from navLabel/text
        label = navpoint.find("ncx:navLabel/ncx:text", NAMESPACES)
        content = navpoint.find("ncx:content", NAMESPACES)

        if label is not None and label.text and content is not None:
            src = content.get("src")
            if src:
                text = label.text.strip()
                # Remove fragment identifier for matching
                base_src = src.split("#")[0]
                if base_src:
                    full_href = _resolve_path(opf_dir, base_src)
                    titles[base_src] = text
                    titles[full_href] = text

    return titles


def read_chapter(path: Path, chapter_href: str) -> str:
    """
    Read the content of a specific chapter from an EPUB file.

    Args:
        path: Path to the EPUB file
        chapter_href: The href of the chapter (as returned by list_chapters)

    Returns:
        The raw XHTML content of the chapter

    Raises:
        FileNotFoundError: If the chapter doesn't exist in the EPUB
        zipfile.BadZipFile: If the EPUB is corrupt
    """
    with zipfile.ZipFile(path, "r") as zf:
        try:
            return zf.read(chapter_href).decode("utf-8")
        except KeyError:
            raise FileNotFoundError(f"Chapter not found: {chapter_href}")
