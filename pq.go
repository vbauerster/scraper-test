package scraper

type boundsCounter struct {
	name  string
	index int
	count uint
}

type boundsHeap []*boundsCounter

func (h boundsHeap) Len() int { return len(h) }

func (h boundsHeap) Less(i, j int) bool {
	return h[i].count > h[j].count
}

func (h boundsHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *boundsHeap) Push(x interface{}) {
	s := *h
	bar := x.(*boundsCounter)
	bar.index = len(s)
	s = append(s, bar)
	*h = s
}

func (h *boundsHeap) Pop() interface{} {
	s := *h
	*h = s[0 : len(s)-1]
	bar := s[len(s)-1]
	bar.index = -1 // for safety
	return bar
}
