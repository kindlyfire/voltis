package metadata

import (
	"reflect"
	"strings"
)

type Staff struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// CoverRef points to a provider cover. Its URL must change with the image, as the cache keys on it.
type CoverRef struct {
	URL string `json:"url"`
}

type ContentRating string

const (
	Safe         ContentRating = "safe"
	Suggestive   ContentRating = "suggestive"
	Erotica      ContentRating = "erotica"
	Pornographic ContentRating = "pornographic"
)

var ContentRatings = []ContentRating{Safe, Suggestive, Erotica, Pornographic}

type Status string

const (
	Releasing Status = "releasing"
	Completed Status = "completed"
	Hiatus    Status = "hiatus"
	Cancelled Status = "cancelled"
	Upcoming  Status = "upcoming"
)

var Statuses = []Status{Releasing, Completed, Hiatus, Cancelled, Upcoming}

// Active reports whether more volumes may still come.
func (s Status) Active() bool { return s == Releasing || s == Upcoming || s == Hiatus }

type Kind string

const (
	Manga  Kind = "manga"
	Manhwa Kind = "manhwa"
	Manhua Kind = "manhua"
	OEL    Kind = "oel"
	Comic  Kind = "comic"
	Novel  Kind = "novel"
)

var Kinds = []Kind{Manga, Manhwa, Manhua, OEL, Comic, Novel}

type Fields struct {
	Title           Opt[string]        `json:"title,omitzero"`
	AltTitles       Opt[[]string]      `json:"alt_titles,omitzero"`
	Description     Opt[string]        `json:"description,omitzero"`
	Staff           Opt[[]Staff]       `json:"staff,omitzero"`
	Publishers      Opt[[]string]      `json:"publishers,omitzero"`
	Language        Opt[string]        `json:"language,omitzero"`
	PublicationDate Opt[string]        `json:"publication_date,omitzero"` // partial ISO
	Genres          Opt[[]string]      `json:"genres,omitzero"`           // slugs
	Tags            Opt[[]string]      `json:"tags,omitzero"`
	ContentRating   Opt[ContentRating] `json:"content_rating,omitzero"`
	Status          Opt[Status]        `json:"status,omitzero"`
	Kind            Opt[Kind]          `json:"kind,omitzero"`
	Rating          Opt[float64]       `json:"rating,omitzero"` // 0-100
	Links           Opt[[]Link]        `json:"links,omitzero"`
	Cover           Opt[CoverRef]      `json:"cover,omitzero"`

	Series          Opt[string]  `json:"series,omitzero"`
	Number          Opt[string]  `json:"number,omitzero"`
	Volume          Opt[string]  `json:"volume,omitzero"`
	Count           Opt[int]     `json:"count,omitzero"`
	SeriesIndex     Opt[float64] `json:"series_index,omitzero"`
	Imprint         Opt[string]  `json:"imprint,omitzero"`
	Format          Opt[string]  `json:"format,omitzero"`
	Web             Opt[string]  `json:"web,omitzero"`
	Notes           Opt[string]  `json:"notes,omitzero"`
	ScanInformation Opt[string]  `json:"scan_information,omitzero"`
	BlackAndWhite   Opt[string]  `json:"black_and_white,omitzero"`
	SeriesGroup     Opt[string]  `json:"series_group,omitzero"`
	AlternateSeries Opt[string]  `json:"alternate_series,omitzero"`
	AlternateNumber Opt[string]  `json:"alternate_number,omitzero"`
	AlternateCount  Opt[int]     `json:"alternate_count,omitzero"`
	Manga           Opt[string]  `json:"manga,omitzero"`
}

type FieldType string

const (
	TypeString     FieldType = "string"
	TypeText       FieldType = "text"
	TypeInt        FieldType = "int"
	TypeFloat      FieldType = "float"
	TypeDate       FieldType = "date"
	TypeStringList FieldType = "string_list"
	TypeGenreList  FieldType = "genre_list"
	TypeStaff      FieldType = "staff"
	TypeEnum       FieldType = "enum"
	TypeLinks      FieldType = "links"
	TypeCover      FieldType = "cover"
)

type FieldDef struct {
	Key          string    `json:"key"`
	Label        string    `json:"label"`
	Type         FieldType `json:"type"`
	Options      []string  `json:"options,omitempty"` // enum values; staff roles
	Min          *float64  `json:"min,omitempty"`
	Max          *float64  `json:"max,omitempty"`
	ContentTypes []string  `json:"content_types"`
	Editable     bool      `json:"editable"`
	Union        bool      `json:"-"`
}

var (
	allTypes   = []string{"comic", "comic_series", "book", "book_series"}
	comicTypes = []string{"comic"}
	// SeriesTypes are the content types provider data attaches to.
	SeriesTypes = []string{"comic_series", "book_series"}
)

var StaffRoles = []string{"author", "artist", "writer", "penciller", "inker", "colorist", "letterer",
	"cover_artist", "editor", "translator"}

var defs = []FieldDef{
	{Key: "title", Label: "Title", Type: TypeString, ContentTypes: allTypes, Editable: true},
	{Key: "alt_titles", Label: "Alternative titles", Type: TypeStringList, ContentTypes: allTypes, Union: true},
	{Key: "description", Label: "Description", Type: TypeText, ContentTypes: allTypes, Editable: true},
	{Key: "staff", Label: "Staff", Type: TypeStaff, Options: StaffRoles, ContentTypes: allTypes, Editable: true},
	{Key: "publishers", Label: "Publishers", Type: TypeStringList, ContentTypes: allTypes, Editable: true},
	{Key: "language", Label: "Language", Type: TypeString, ContentTypes: allTypes, Editable: true},
	{Key: "publication_date", Label: "Publication date", Type: TypeDate, ContentTypes: allTypes, Editable: true},
	{Key: "genres", Label: "Genres", Type: TypeGenreList, ContentTypes: allTypes, Editable: true},
	{Key: "tags", Label: "Tags", Type: TypeStringList, ContentTypes: allTypes, Editable: true},
	{Key: "content_rating", Label: "Content rating", Type: TypeEnum, Options: strs(ContentRatings),
		ContentTypes: allTypes, Editable: true},
	{Key: "status", Label: "Status", Type: TypeEnum, Options: strs(Statuses), ContentTypes: SeriesTypes, Editable: true},
	{Key: "kind", Label: "Kind", Type: TypeEnum, Options: strs(Kinds), ContentTypes: SeriesTypes, Editable: true},
	{Key: "rating", Label: "Rating", Type: TypeFloat, Min: new(0.0), Max: new(100.0), ContentTypes: allTypes, Editable: true},
	{Key: "links", Label: "Links", Type: TypeLinks, ContentTypes: SeriesTypes, Union: true},
	// Only provider layers set a cover; an override can only clear it, restoring the local one.
	{Key: "cover", Label: "Cover", Type: TypeCover, ContentTypes: SeriesTypes, Editable: true},
	{Key: "series", Label: "Series", Type: TypeString, ContentTypes: []string{"comic", "book"}, Editable: true},
	{Key: "number", Label: "Number", Type: TypeString, ContentTypes: comicTypes, Editable: true},
	{Key: "volume", Label: "Volume", Type: TypeString, ContentTypes: comicTypes, Editable: true},
	{Key: "count", Label: "Count", Type: TypeInt, Min: new(0.0), ContentTypes: []string{"comic", "comic_series", "book_series"},
		Editable: true},
	{Key: "series_index", Label: "Series index", Type: TypeFloat, ContentTypes: []string{"book"}, Editable: true},
	{Key: "imprint", Label: "Imprint", Type: TypeString, ContentTypes: comicTypes, Editable: true},
	{Key: "format", Label: "Format", Type: TypeString, ContentTypes: comicTypes, Editable: true},
	{Key: "web", Label: "Web", Type: TypeString, ContentTypes: comicTypes, Editable: true},
	{Key: "notes", Label: "Notes", Type: TypeText, ContentTypes: comicTypes, Editable: true},
	{Key: "scan_information", Label: "Scan information", Type: TypeString, ContentTypes: comicTypes, Editable: true},
	{Key: "black_and_white", Label: "Black and white", Type: TypeString, ContentTypes: comicTypes, Editable: true},
	{Key: "series_group", Label: "Series group", Type: TypeString, ContentTypes: comicTypes, Editable: true},
	{Key: "alternate_series", Label: "Alternate series", Type: TypeString, ContentTypes: comicTypes, Editable: true},
	{Key: "alternate_number", Label: "Alternate number", Type: TypeString, ContentTypes: comicTypes, Editable: true},
	{Key: "alternate_count", Label: "Alternate count", Type: TypeInt, Min: new(0.0), ContentTypes: comicTypes,
		Editable: true},
	{Key: "manga", Label: "Manga", Type: TypeString, ContentTypes: comicTypes, Editable: true},
}

// Defs lists every field in display and merge order.
func Defs() []FieldDef { return defs }

func strs[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

// fieldIndex maps a JSON key to its Fields member.
var fieldIndex = func() map[string]int {
	m := map[string]int{}
	typ := reflect.TypeFor[Fields]()
	for i := range typ.NumField() {
		key, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		m[key] = i
	}
	return m
}()

// field returns the Opt member for a key.
func (f *Fields) field(key string) reflect.Value {
	return reflect.ValueOf(f).Elem().Field(fieldIndex[key])
}

func presence(opt reflect.Value) Presence { return Presence(opt.Field(0).Uint()) }

// IsZero reports whether every field is absent.
func (f Fields) IsZero() bool {
	v := reflect.ValueOf(f)
	for i := range v.NumField() {
		if presence(v.Field(i)) != Absent {
			return false
		}
	}
	return true
}
