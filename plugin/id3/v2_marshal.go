package id3

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type v2RawFrame struct {
	block    metadata.RawBlock
	position int
	order    int
}

type v2SemanticFrame struct {
	data   []byte
	size   int
	append func([]byte) []byte
	units  int
}

func marshalV2Canonical(document metadata.Document, layout *v2Layout, slot carrier.ID, encoding plugin.Identity) (metadata.Blob, []loss.Loss, error) {
	semantic, lost := v2CanonicalSemanticFrames(document.Entries())
	semanticUnits := 0
	for _, frame := range semantic {
		semanticUnits += frame.units
	}
	raw, err := v2CanonicalRawFrames(document, layout, slot, encoding, semanticUnits)
	if err != nil {
		return metadata.Blob{}, nil, err
	}
	frames := v2InterleaveFrames(semantic, raw)
	if len(frames) == 0 {
		return metadata.NewBlob(v2MediaType, nil), lost, nil
	}
	payloadSize := 0
	for _, frame := range frames {
		payloadSize += frame.Len()
	}
	if payloadSize > 0x0fffffff {
		return metadata.Blob{}, nil, fmt.Errorf("%w: ID3v2 canonical tag exceeds syncsafe size", errV2Unsupported)
	}
	value := make([]byte, 0, v2HeaderSize+payloadSize)
	value = append(value, 'I', 'D', '3', 4, 0, 0)
	value = append(value, v2EncodeSyncSafe(payloadSize)...)
	for _, frame := range frames {
		value = frame.AppendTo(value)
	}
	return metadata.NewBlob(v2MediaType, value), lost, nil
}

func (f v2SemanticFrame) Len() int {
	if f.append != nil {
		return f.size
	}
	return len(f.data)
}

func (f v2SemanticFrame) AppendTo(destination []byte) []byte {
	if f.append != nil {
		return f.append(destination)
	}
	return append(destination, f.data...)
}

type v2OutputFrame struct {
	semantic *v2SemanticFrame
	raw      metadata.Blob
}

func (f v2OutputFrame) Len() int {
	if f.semantic != nil {
		return f.semantic.Len()
	}
	return f.raw.Len()
}

func (f v2OutputFrame) AppendTo(destination []byte) []byte {
	if f.semantic != nil {
		return f.semantic.AppendTo(destination)
	}
	return f.raw.AppendTo(destination)
}

type v2PairPlan struct {
	totalForNumber map[int]int
	extraTotals    map[int]bool
}

func v2CanonicalSemanticFrames(entries []metadata.Entry) ([]v2SemanticFrame, []loss.Loss) {
	tracks := v2PlanPairs(entries, tag.TrackNumber().ID(), tag.TotalTracks().ID())
	discs := v2PlanPairs(entries, tag.DiscNumber().ID(), tag.TotalDiscs().ID())
	textGroups := v2GroupSetForPartition(entries, func(entry metadata.Entry) bool {
		_, ok := v2OutputTextFrame(entry.Key())
		return ok
	}, func(_ int, entry metadata.Entry) string {
		frame, _ := v2OutputTextFrame(entry.Key())
		return frame
	})
	dateGroups := v2GroupSetFor(entries, func(entry metadata.Entry) bool { return entry.Key() == tag.Date().ID() })
	trackGroups := v2GroupSetFor(entries, func(entry metadata.Entry) bool { return entry.Key() == tag.TrackNumber().ID() })
	discGroups := v2GroupSetFor(entries, func(entry metadata.Entry) bool { return entry.Key() == tag.DiscNumber().ID() })
	commentGroups := v2GroupSetFor(entries, func(entry metadata.Entry) bool { return entry.Key() == tag.Comment().ID() })
	lyricsGroups := v2GroupSetFor(entries, func(entry metadata.Entry) bool { return entry.Key() == tag.Lyrics().ID() })
	websiteGroups := v2GroupSetFor(entries, func(entry metadata.Entry) bool { return entry.Key() == tag.Website().ID() })
	pictureGroups := v2PictureGroups(entries)
	pairedTotals := v2PairedTotals(tracks, discs)
	var frames []v2SemanticFrame
	var lost []loss.Loss
	for index, entry := range entries {
		if frameID, ok := v2OutputTextFrame(entry.Key()); ok {
			indexes, first := textGroups.first(index)
			if !first {
				continue
			}
			frame, reports := v2CanonicalTextGroup(entries, indexes, frameID)
			frames = append(frames, frame...)
			lost = append(lost, reports...)
			continue
		}
		switch entry.Key() {
		case tag.Date().ID():
			indexes, first := dateGroups.first(index)
			if !first {
				continue
			}
			frame, reports := v2CanonicalDateGroup(entries, indexes)
			frames = append(frames, frame...)
			lost = append(lost, reports...)
		case tag.Comment().ID():
			indexes, first := commentGroups.first(index)
			if !first {
				continue
			}
			frame, reports := v2CanonicalUniqueGroup(entries, indexes, "COMM", "id3v2.qualifier-folded", v2CanonicalQualifiedFrame)
			frames = append(frames, frame...)
			lost = append(lost, reports...)
		case tag.Lyrics().ID():
			indexes, first := lyricsGroups.first(index)
			if !first {
				continue
			}
			frame, reports := v2CanonicalUniqueGroup(entries, indexes, "USLT", "id3v2.qualifier-folded", v2CanonicalQualifiedFrame)
			frames = append(frames, frame...)
			lost = append(lost, reports...)
		case tag.Website().ID():
			indexes, first := websiteGroups.first(index)
			if !first {
				continue
			}
			frame, reports := v2CanonicalUniqueGroup(entries, indexes, "WXXX", "id3v2.qualifier-folded", v2CanonicalURLFrame)
			frames = append(frames, frame...)
			lost = append(lost, reports...)
		case tag.Picture().ID():
			indexes, first := pictureGroups.first(index)
			if !first {
				continue
			}
			frame, reports := v2CanonicalUniqueGroup(entries, indexes, "APIC", "id3v2.picture-folded", v2CanonicalPictureFrame)
			frames = append(frames, frame...)
			lost = append(lost, reports...)
		case tag.TrackNumber().ID():
			indexes, first := trackGroups.first(index)
			if !first {
				continue
			}
			frame, reports := v2CanonicalOrdinalGroup(entries, indexes, tracks, "TRCK")
			frames = append(frames, frame...)
			lost = append(lost, reports...)
		case tag.DiscNumber().ID():
			indexes, first := discGroups.first(index)
			if !first {
				continue
			}
			frame, reports := v2CanonicalOrdinalGroup(entries, indexes, discs, "TPOS")
			frames = append(frames, frame...)
			lost = append(lost, reports...)
		case tag.TotalTracks().ID():
			if pairedTotals[index] {
				continue
			}
			if tracks.extraTotals[index] {
				lost = append(lost, v2Loss(entry, loss.Dropped, "TRCK", "id3v2.total-without-number"))
			}
		case tag.TotalDiscs().ID():
			if pairedTotals[index] {
				continue
			}
			if discs.extraTotals[index] {
				lost = append(lost, v2Loss(entry, loss.Dropped, "TPOS", "id3v2.total-without-number"))
			}
		default:
			lost = append(lost, v2Loss(entry, loss.Dropped, "", "id3v2.unrepresentable"))
		}
	}
	return frames, lost
}

type v2GroupSet struct {
	at map[int][]int
}

func v2GroupSetForPartition(entries []metadata.Entry, selectEntry func(metadata.Entry) bool, partition func(int, metadata.Entry) string) v2GroupSet {
	result := v2GroupSet{at: make(map[int][]int)}
	byKey := make(map[string][]int)
	for index, entry := range entries {
		if selectEntry(entry) {
			key := partition(index, entry)
			byKey[key] = append(byKey[key], index)
		}
	}
	for _, indexes := range byKey {
		result.at[indexes[0]] = indexes
	}
	return result
}

func v2GroupSetFor(entries []metadata.Entry, selectEntry func(metadata.Entry) bool) v2GroupSet {
	return v2GroupSetForPartition(entries, selectEntry, func(int, metadata.Entry) string { return "one" })
}

func (g v2GroupSet) first(index int) ([]int, bool) {
	values, ok := g.at[index]
	return values, ok
}

func v2PictureGroups(entries []metadata.Entry) v2GroupSet {
	return v2GroupSetForPartition(entries, func(entry metadata.Entry) bool { return entry.Key() == tag.Picture().ID() }, func(index int, entry metadata.Entry) string {
		picture, ok := entry.Value().(tag.Artwork)
		if !ok {
			return fmt.Sprintf("invalid/%d", index)
		}
		if picture.Type == tag.ArtworkFileIcon || picture.Type == 2 {
			return fmt.Sprintf("type/%d", picture.Type)
		}
		description, _ := v2CanonicalText(picture.Description)
		return "description/" + description
	})
}

func v2PairedTotals(plans ...v2PairPlan) map[int]bool {
	result := make(map[int]bool)
	for _, plan := range plans {
		for _, total := range plan.totalForNumber {
			result[total] = true
		}
	}
	return result
}

func v2OutputTextFrame(identity key.ID) (string, bool) {
	switch identity {
	case tag.Title().ID():
		return "TIT2", true
	case tag.Artist().ID():
		return "TPE1", true
	case tag.Album().ID():
		return "TALB", true
	case tag.Genre().ID():
		return "TCON", true
	case tag.Composer().ID():
		return "TCOM", true
	case tag.Copyright().ID():
		return "TCOP", true
	case tag.Encoder().ID():
		return "TENC", true
	}
	return "", false
}

func v2CanonicalTextGroup(entries []metadata.Entry, indexes []int, frameID string) ([]v2SemanticFrame, []loss.Loss) {
	var values []string
	var reports []loss.Loss
	for _, index := range indexes {
		entry := entries[index]
		value, ok := entry.Value().(string)
		if !ok || value == "" {
			reports = append(reports, v2Loss(entry, loss.Dropped, frameID, "id3v2.text-unrepresentable"))
			continue
		}
		value, substituted := v2CanonicalText(value)
		values = append(values, value)
		if substituted {
			reports = append(reports, v2Loss(entry, loss.Substituted, frameID, "id3v2.text-substituted"))
		}
	}
	if len(values) == 0 {
		return nil, reports
	}
	payload := append([]byte{3}, []byte(strings.Join(values, "\x00"))...)
	return []v2SemanticFrame{{data: v2BuildFrame(frameID, payload), units: len(indexes)}}, reports
}

func v2CanonicalDateGroup(entries []metadata.Entry, indexes []int) ([]v2SemanticFrame, []loss.Loss) {
	var values []string
	var reports []loss.Loss
	for _, index := range indexes {
		entry := entries[index]
		value, ok := entry.Value().(tag.PartialDate)
		if !ok || !value.HasValue() || value.ToISOString() == "" {
			reports = append(reports, v2Loss(entry, loss.Dropped, "TDRC", "id3v2.date-unrepresentable"))
			continue
		}
		values = append(values, value.ToISOString())
	}
	if len(values) == 0 {
		return nil, reports
	}
	payload := append([]byte{3}, []byte(strings.Join(values, "\x00"))...)
	return []v2SemanticFrame{{data: v2BuildFrame("TDRC", payload), units: len(indexes)}}, reports
}

type v2FrameEmitter func(metadata.Entry, string) ([]v2SemanticFrame, []loss.Loss)

func v2CanonicalUniqueGroup(entries []metadata.Entry, indexes []int, frameID, detail string, emit v2FrameEmitter) ([]v2SemanticFrame, []loss.Loss) {
	var frames []v2SemanticFrame
	var reports []loss.Loss
	selected := false
	for _, index := range indexes {
		entry := entries[index]
		candidate, candidateReports := emit(entry, frameID)
		if len(candidate) == 0 {
			reports = append(reports, candidateReports...)
			continue
		}
		if selected {
			reports = append(reports, v2Loss(entry, loss.Folded, frameID, detail))
			continue
		}
		selected = true
		frames = append(frames, candidate...)
		reports = append(reports, candidateReports...)
	}
	if len(frames) != 0 {
		frames[0].units = len(indexes)
	}
	return frames, reports
}

func v2CanonicalQualifiedFrame(entry metadata.Entry, frameID string) ([]v2SemanticFrame, []loss.Loss) {
	value, ok := entry.Value().(string)
	if !ok || value == "" {
		return nil, []loss.Loss{v2Loss(entry, loss.Dropped, frameID, "id3v2.text-unrepresentable")}
	}
	value, substituted := v2CanonicalText(value)
	payload := append([]byte{3, 'X', 'X', 'X', 0}, []byte(value)...)
	frame := v2SemanticFrame{data: v2BuildFrame(frameID, payload), units: 1}
	if substituted {
		return []v2SemanticFrame{frame}, []loss.Loss{v2Loss(entry, loss.Substituted, frameID, "id3v2.text-substituted")}
	}
	return []v2SemanticFrame{frame}, nil
}

func v2CanonicalURLFrame(entry metadata.Entry, _ string) ([]v2SemanticFrame, []loss.Loss) {
	value, ok := entry.Value().(string)
	if !ok || value == "" {
		return nil, []loss.Loss{v2Loss(entry, loss.Dropped, "WXXX", "id3v2.url-unrepresentable")}
	}
	encoded, substituted := v2EncodeLatin1(value)
	payload := append([]byte{3, 0}, encoded...)
	frame := v2SemanticFrame{data: v2BuildFrame("WXXX", payload), units: 1}
	if substituted {
		return []v2SemanticFrame{frame}, []loss.Loss{v2Loss(entry, loss.Substituted, "WXXX", "id3v2.url-substituted")}
	}
	return []v2SemanticFrame{frame}, nil
}

func v2CanonicalPictureFrame(entry metadata.Entry, _ string) ([]v2SemanticFrame, []loss.Loss) {
	picture, ok := entry.Value().(tag.Artwork)
	mediaType := picture.EffectiveMediaType()
	if !ok || !picture.Valid() || picture.Data.Len() == 0 || !v2MediaTypeValid(mediaType) || picture.Type > 0x14 {
		return nil, []loss.Loss{v2Loss(entry, loss.Dropped, "APIC", "id3v2.picture-unrepresentable")}
	}
	description, substituted := v2CanonicalText(picture.Description)
	prefix := make([]byte, 0, 3+len(mediaType)+len(description))
	prefix = append(prefix, 3)
	prefix = append(prefix, mediaType...)
	prefix = append(prefix, 0, byte(picture.Type))
	prefix = append(prefix, description...)
	prefix = append(prefix, 0)
	image := picture.Data
	frame := v2SemanticFrame{
		size: 10 + len(prefix) + image.Len(),
		append: func(destination []byte) []byte {
			destination = v2AppendFrame(destination, "APIC", len(prefix)+image.Len())
			destination = append(destination, prefix...)
			return image.AppendTo(destination)
		},
		units: 1,
	}
	var reports []loss.Loss
	if substituted {
		reports = append(reports, v2Loss(entry, loss.Substituted, "APIC", "id3v2.text-substituted"))
	}
	if picture.Width != 0 || picture.Height != 0 || picture.ColorDepth != 0 || picture.IndexedColors != 0 {
		reports = append(reports, v2Loss(entry, loss.Truncated, "APIC", "id3v2.apic-dimensions"))
	}
	return []v2SemanticFrame{frame}, reports
}

func v2PlanPairs(entries []metadata.Entry, number, total key.ID) v2PairPlan {
	result := v2PairPlan{totalForNumber: make(map[int]int), extraTotals: make(map[int]bool)}
	var numbers, totals []int
	for index, entry := range entries {
		if entry.Key() == number {
			numbers = append(numbers, index)
		}
		if entry.Key() == total {
			totals = append(totals, index)
		}
	}
	for index, totalIndex := range totals {
		if index < len(numbers) {
			result.totalForNumber[numbers[index]] = totalIndex
			continue
		}
		result.extraTotals[totalIndex] = true
	}
	return result
}

func v2NonnegativeOrdinal(entry metadata.Entry) (int64, bool) {
	value, ok := entry.Value().(int64)
	return value, ok && value >= 0
}

func v2CanonicalOrdinalGroup(entries []metadata.Entry, indexes []int, plan v2PairPlan, frameID string) ([]v2SemanticFrame, []loss.Loss) {
	units := len(indexes)
	for _, index := range indexes {
		if _, ok := plan.totalForNumber[index]; ok {
			units++
		}
	}
	var values []string
	var reports []loss.Loss
	for _, index := range indexes {
		entry := entries[index]
		number, ok := v2NonnegativeOrdinal(entry)
		totalIndex, hasTotal := plan.totalForNumber[index]
		if !ok {
			reports = append(reports, v2Loss(entry, loss.Dropped, frameID, "id3v2.number-unrepresentable"))
			if hasTotal {
				reports = append(reports, v2Loss(entries[totalIndex], loss.Dropped, frameID, "id3v2.total-without-number"))
			}
			continue
		}
		value := strconv.FormatInt(number, 10)
		if hasTotal {
			total, valid := v2NonnegativeOrdinal(entries[totalIndex])
			if valid {
				value += "/" + strconv.FormatInt(total, 10)
			} else {
				reports = append(reports, v2Loss(entries[totalIndex], loss.Dropped, frameID, "id3v2.total-unrepresentable"))
			}
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, reports
	}
	payload := append([]byte{3}, []byte(strings.Join(values, "\x00"))...)
	return []v2SemanticFrame{{data: v2BuildFrame(frameID, payload), units: units}}, reports
}

func v2CanonicalRawFrames(document metadata.Document, layout *v2Layout, slot carrier.ID, encoding plugin.Identity, semanticCount int) ([]v2RawFrame, error) {
	var result []v2RawFrame
	for _, block := range document.Blocks() {
		if block.Source() {
			continue
		}
		if block.Carrier() != slot || block.Encoding() != encoding || block.Payload().MediaType() != v2RawMediaType || !v2SafeRawFrame(block.Payload().AppendTo(nil)) {
			return nil, fmt.Errorf("%w: ID3v2 cannot migrate opaque metadata block %s", errV2Unsupported, block.ID())
		}
		position := semanticCount
		if layout != nil {
			if value, ok := layout.opaque[block.ID()]; ok {
				position = value
			}
		}
		result = append(result, v2RawFrame{block: block, position: position, order: len(result)})
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].position != result[right].position {
			return result[left].position < result[right].position
		}
		return result[left].order < result[right].order
	})
	return result, nil
}

func v2InterleaveFrames(semantic []v2SemanticFrame, raw []v2RawFrame) []v2OutputFrame {
	result := make([]v2OutputFrame, 0, len(semantic)+len(raw))
	rawIndex := 0
	units := 0
	for index := range semantic {
		frame := &semantic[index]
		for rawIndex < len(raw) && raw[rawIndex].position <= units {
			result = append(result, v2OutputFrame{raw: raw[rawIndex].block.Payload()})
			rawIndex++
		}
		result = append(result, v2OutputFrame{semantic: frame})
		units += frame.units
	}
	for rawIndex < len(raw) {
		result = append(result, v2OutputFrame{raw: raw[rawIndex].block.Payload()})
		rawIndex++
	}
	return result
}
