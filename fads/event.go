package fads

type Event struct {
	Header RunHeader
}

type RunHeader struct {
	RunNbr  int64 // run number
	EvtNbr  int64 // event number
	Trigger int64 // trigger word
}

// EOF
