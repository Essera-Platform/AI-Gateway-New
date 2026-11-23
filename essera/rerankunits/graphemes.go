package rerankunits

import (
	"sort"
	"unicode/utf8"
)

// Extended grapheme clusters as Unicode 17's UAX #29 defines them, with the rules and
// lookbehinds of the Rust crate unicode-segmentation that Hugging Face tokenizers uses. Go's
// own tables and the Go libraries are older; a mark Unicode 17 added would otherwise split a
// cluster that tokenizers keeps whole, and change a count.

type graphemeCategory uint8

const (
	gcAny graphemeCategory = iota
	gcCR
	gcControl
	gcExtend
	gcExtendedPictographic
	gcInCBConsonant
	gcL
	gcLF
	gcLV
	gcLVT
	gcPrepend
	gcRegionalIndicator
	gcSpacingMark
	gcT
	gcV
	gcZWJ
)

type categoryRange struct {
	low, high rune
	category  graphemeCategory
}

type runeRange struct{ low, high rune }

func categoryOf(r rune) graphemeCategory {
	i := sort.Search(len(graphemeCategories), func(i int) bool { return graphemeCategories[i].high >= r })
	if i < len(graphemeCategories) && graphemeCategories[i].low <= r {
		return graphemeCategories[i].category
	}
	return gcAny
}

func isInCBExtend(r rune) bool {
	i := sort.Search(len(incbExtend), func(i int) bool { return incbExtend[i].high >= r })
	return i < len(incbExtend) && incbExtend[i].low <= r
}

// graphemeBreak tells whether a cluster ends at offset of text, between before and after.
func graphemeBreak(text string, offset int, before, after graphemeCategory) bool {
	switch {
	case before == gcCR && after == gcLF:
		return false // GB3
	case before == gcControl || before == gcCR || before == gcLF:
		return true // GB4
	case after == gcControl || after == gcCR || after == gcLF:
		return true // GB5
	case before == gcL && (after == gcL || after == gcV || after == gcLV || after == gcLVT):
		return false // GB6
	case (before == gcLV || before == gcV) && (after == gcV || after == gcT):
		return false // GB7
	case (before == gcLVT || before == gcT) && after == gcT:
		return false // GB8
	case after == gcExtend || after == gcZWJ:
		return false // GB9
	case after == gcSpacingMark:
		return false // GB9a
	case before == gcPrepend:
		return false // GB9b
	case after == gcInCBConsonant:
		return !linkedConsonantBefore(text[:offset]) // GB9c
	case before == gcZWJ && after == gcExtendedPictographic:
		return !pictographBefore(text[:offset]) // GB11
	case before == gcRegionalIndicator && after == gcRegionalIndicator:
		return regionalIndicatorsBefore(text[:offset])%2 == 0 // GB12, GB13
	}
	return true // GB999
}

// linkedConsonantBefore: an InCB consonant, then InCB extends and linkers with at least one
// linker, end the text.
func linkedConsonantBefore(text string) bool {
	linkers := 0
	for text != "" {
		r, size := utf8.DecodeLastRuneInString(text)
		text = text[:len(text)-size]
		switch {
		case incbLinkers[r]:
			linkers++
		case isInCBExtend(r):
		default:
			return linkers > 0 && categoryOf(r) == gcInCBConsonant
		}
	}
	return false
}

// pictographBefore: an Extended_Pictographic, then extends, then the ZWJ ending the text.
func pictographBefore(text string) bool {
	_, size := utf8.DecodeLastRuneInString(text)
	text = text[:len(text)-size]
	for text != "" {
		r, size := utf8.DecodeLastRuneInString(text)
		text = text[:len(text)-size]
		switch categoryOf(r) {
		case gcExtend:
		case gcExtendedPictographic:
			return true
		default:
			return false
		}
	}
	return false
}

func regionalIndicatorsBefore(text string) int {
	count := 0
	for text != "" {
		r, size := utf8.DecodeLastRuneInString(text)
		if categoryOf(r) != gcRegionalIndicator {
			break
		}
		count++
		text = text[:len(text)-size]
	}
	return count
}

// graphemes splits text into extended grapheme clusters.
func graphemes(text string, each func(cluster string)) {
	start := 0
	previous := gcAny
	for offset, r := range text {
		category := categoryOf(r)
		if offset > 0 && graphemeBreak(text, offset, previous, category) {
			each(text[start:offset])
			start = offset
		}
		previous = category
	}
	if start < len(text) {
		each(text[start:])
	}
}
