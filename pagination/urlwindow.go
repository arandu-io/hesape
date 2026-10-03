package pagination

// URLWindow is the arithmetic that decides which page numbers a numbered pager
// shows when there are too many to show them all.
//
// It reports three pieces -- the first pages, the pages around the current
// one, and the last pages -- and the renderer puts a separator wherever two
// pieces do not meet. Below a certain number of pages there is no window at
// all: every page fits, so every page is listed.
//
// It is built over a LengthAwarePage, which carries the three numbers the window
// depends on and the options its URLs are written with.
type URLWindow struct {
	page *LengthAwarePage
}

// URLWindowRanges is three page-to-URL ranges, any of which may be absent.
//
// A range that does not apply is a nil map. Each range is keyed by page number
// -- Go maps have no order, so a renderer walking one sorts the keys, which is
// what the page numbers are for.
type URLWindowRanges struct {
	// First is the opening run of pages: page one and two when there is a
	// slider, the whole range when there are few enough pages, and a wider run
	// when the current page is close to the beginning.
	First map[int]string

	// Slider is the run of pages around the current one, and is absent unless
	// there is room for a full window on both sides.
	Slider map[int]string

	// Last is the closing run of pages, and is absent when the first run
	// already reaches the end.
	Last map[int]string
}

// NewURLWindow builds the window for a length-aware page.
func NewURLWindow(page *LengthAwarePage) *URLWindow {
	return &URLWindow{page: page}
}

// Make is NewURLWindow followed by Get, in one call.
func Make(page *LengthAwarePage) URLWindowRanges {
	return NewURLWindow(page).Get()
}

// Get is the window of URLs to be shown.
func (w *URLWindow) Get() URLWindowRanges {
	first, slider, last := w.pages()
	return URLWindowRanges{
		First:  w.page.urlsFor(first),
		Slider: w.page.urlsFor(slider),
		Last:   w.page.urlsFor(last),
	}
}

// pages is Get in page numbers rather than URLs, and in reading order.
//
// It exists because the elements a pager renders are a sequence and a Go map is
// not, and because Links and LinkCollection need the same three pieces Get
// returns. Both go through windowPages, so the branching lives once.
func (w *URLWindow) pages() (first, slider, last []int) {
	return windowPages(w.page.currentPage, w.page.lastPage, w.page.onEachSide)
}

// windowPages is the window in page numbers, in reading order: the opening run,
// the slider around current, and the closing run, any of which may be empty.
//
// It depends on nothing but the three numbers, so every pager that renders a
// numbered window asks it rather than carrying a copy of the branching.
func windowPages(current, lastPage, onEachSide int) (first, slider, last []int) {
	if lastPage < onEachSide*2+8 {
		// The small slider: not enough pages to leave any out.
		return pageRange(1, lastPage), nil, nil
	}

	width := onEachSide + 4

	if lastPage <= 1 {
		return nil, nil, nil
	}

	start := pageRange(1, 2)
	finish := pageRange(lastPage-1, lastPage)

	switch {
	// Close to the beginning: render the opening run, then the last two, since
	// there is no room for a full slider on the left.
	case current <= width:
		return pageRange(1, width+onEachSide), nil, finish

	// Close to the end: the first two, then a wider closing run.
	case current > lastPage-width:
		return start, nil, pageRange(lastPage-(width+onEachSide-1), lastPage)

	// Room on both sides: caps at each end and a sliding window in the middle.
	default:
		return start, pageRange(current-onEachSide, current+onEachSide), finish
	}
}

// GetAdjacentURLRange is the run of pages onEachSide either side of the
// current one.
func (w *URLWindow) GetAdjacentURLRange(onEachSide int) map[int]string {
	return w.page.urlsFor(w.adjacent(onEachSide))
}

// GetStart is the first two pages, the cap at the beginning of a slider.
func (w *URLWindow) GetStart() map[int]string {
	return w.page.urlsFor(w.start())
}

// GetFinish is the last two pages, the cap at the end of a slider.
func (w *URLWindow) GetFinish() map[int]string {
	return w.page.urlsFor(w.finish())
}

// HasPages reports whether the page being presented belongs to a result set
// spanning more than one page.
//
// It is not the page's own HasPages: this one asks only about the number of
// pages, where the page's is also true for a reader sitting past the end of a
// single-page result set.
func (w *URLWindow) HasPages() bool { return w.page.LastPage() > 1 }

func (w *URLWindow) start() []int { return pageRange(1, 2) }

func (w *URLWindow) finish() []int {
	return pageRange(w.page.LastPage()-1, w.page.LastPage())
}

func (w *URLWindow) adjacent(onEachSide int) []int {
	return pageRange(w.page.CurrentPage()-onEachSide, w.page.CurrentPage()+onEachSide)
}

// pageRange returns the page numbers from start to end inclusive, and nothing
// when the range is empty. A start below one is read as one, which is what
// keeps a window near the beginning from asking for page zero.
func pageRange(start, end int) []int {
	if start < 1 {
		start = 1
	}
	if end < start {
		return nil
	}
	out := make([]int, 0, end-start+1)
	for page := start; page <= end; page++ {
		out = append(out, page)
	}
	return out
}
