package formatter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"

	diff "github.com/yudai/gojsondiff"
)

// A formatter only needs to satisfy the `Format` method, which has the definition,
//
//		Format(diff diff.Diff) (result string, err error)
//
// So that's all we need to do. Ideally, we'd have a custom marshaler.
// Really though, it's easier just to export a few functions which do everything.

func NewAsciiFormatter(left interface{}, config AsciiFormatterConfig) *AsciiFormatter {
	return &AsciiFormatter{
		left:   left,
		config: config,
	}
}

type AsciiFormatter struct {
	left    interface{}
	config  AsciiFormatterConfig
	buffer  *bytes.Buffer
	path    []string
	size    []int
	inArray []bool
	line    *AsciiLine
}

type AsciiFormatterConfig struct {
	ShowArrayIndex bool
	Coloring       bool
}

var AsciiFormatterDefaultConfig = AsciiFormatterConfig{}

type AsciiLine struct {
	marker string
	indent int
	buffer *bytes.Buffer
}

func (f *AsciiFormatter) Format(diff diff.Diff) (result string, err error) {
	f.buffer = bytes.NewBuffer([]byte{})
	f.path = []string{}
	f.size = []int{}
	f.inArray = []bool{}

	if v, ok := f.left.(map[string]interface{}); ok {
		f.formatObject(v, diff)
	} else if v, ok := f.left.([]interface{}); ok {
		f.formatArray(v, diff)
	} else {
		return "", fmt.Errorf("expected map[string]interface{} or []interface{}, got %T",
			f.left)
	}

	return f.buffer.String(), nil
}

func (f *AsciiFormatter) formatObject(left map[string]interface{}, df diff.Diff) {
	f.addLineWith(AsciiSame, "{")
	f.push("ROOT", len(left), false)
	f.processObject(left, df.Deltas())
	f.pop()
	f.addLineWith(AsciiSame, "}")
}

func (f *AsciiFormatter) formatArray(left []interface{}, df diff.Diff) {
	f.addLineWith(AsciiSame, "[")
	f.push("ROOT", len(left), true)
	f.processArray(left, df.Deltas())
	f.pop()
	f.addLineWith(AsciiSame, "]")
}

func (f *AsciiFormatter) processArray(array []interface{}, deltas []diff.Delta) error {
	patchedIndex := 0
	for index, value := range array {
		f.processItem(value, deltas, diff.Index(index))
		patchedIndex++
	}

	// additional Added
	for _, delta := range deltas {
		switch delta.(type) {
		case *diff.Added:
			d := delta.(*diff.Added)
			// skip items already processed
			if int(d.Position.(diff.Index)) < len(array) {
				continue
			}
			f.printRecursive(d.Position.String(), d.Value, AsciiAdded)
		}
	}

	return nil
}

func (f *AsciiFormatter) processObject(object map[string]interface{}, deltas []diff.Delta) error {
	names := sortedKeys(object)
	for _, name := range names {
		value := object[name]
		f.processItem(value, deltas, diff.Name(name))
	}

	// Added
	for _, delta := range deltas {
		switch delta.(type) {
		case *diff.Added:
			d := delta.(*diff.Added)
			f.printRecursive(d.Position.String(), d.Value, AsciiAdded)
		}
	}

	return nil
}

func (f *AsciiFormatter) processItem(value interface{}, deltas []diff.Delta, position diff.Position) error {
	matchedDeltas := f.searchDeltas(deltas, position)
	positionStr := position.String()
	if len(matchedDeltas) > 0 {
		for _, matchedDelta := range matchedDeltas {

			switch matchedDelta.(type) {
			case *diff.Object:
				d := matchedDelta.(*diff.Object)
				switch value.(type) {
				case map[string]interface{}:
					//ok
				default:
					return errors.New("Type mismatch")
				}
				o := value.(map[string]interface{})

				f.newLine(AsciiSame)
				f.printKey(positionStr)
				f.print("{")
				f.closeLine()
				f.push(positionStr, len(o), false)
				f.processObject(o, d.Deltas)
				f.pop()
				f.newLine(AsciiSame)
				f.print("}")
				f.printComma()
				f.closeLine()

			case *diff.Array:
				d := matchedDelta.(*diff.Array)
				switch value.(type) {
				case []interface{}:
					//ok
				default:
					return errors.New("Type mismatch")
				}
				a := value.([]interface{})

				f.newLine(AsciiSame)
				f.printKey(positionStr)
				f.print("[")
				f.closeLine()
				f.push(positionStr, len(a), true)
				f.processArray(a, d.Deltas)
				f.pop()
				f.newLine(AsciiSame)
				f.print("]")
				f.printComma()
				f.closeLine()

			case *diff.Added:
				d := matchedDelta.(*diff.Added)
				f.printRecursive(positionStr, d.Value, AsciiAdded)
				f.size[len(f.size)-1]++

			case *diff.Modified:
				d := matchedDelta.(*diff.Modified)
				savedSize := f.size[len(f.size)-1]
				f.printRecursive(positionStr, d.OldValue, AsciiDeleted)
				f.size[len(f.size)-1] = savedSize
				f.printRecursive(positionStr, d.NewValue, AsciiAdded)

			case *diff.TextDiff:
				savedSize := f.size[len(f.size)-1]
				d := matchedDelta.(*diff.TextDiff)
				f.printRecursive(positionStr, d.OldValue, AsciiDeleted)
				f.size[len(f.size)-1] = savedSize
				f.printRecursive(positionStr, d.NewValue, AsciiAdded)

			case *diff.Deleted:
				d := matchedDelta.(*diff.Deleted)
				f.printRecursive(positionStr, d.Value, AsciiDeleted)

			default:
				return errors.New("Unknown Delta type detected")
			}

		}
	} else {
		f.printRecursive(positionStr, value, AsciiSame)
	}

	return nil
}

func (f *AsciiFormatter) searchDeltas(deltas []diff.Delta, postion diff.Position) (results []diff.Delta) {
	results = make([]diff.Delta, 0)
	for _, delta := range deltas {
		switch delta.(type) {
		case diff.PostDelta:
			if delta.(diff.PostDelta).PostPosition() == postion {
				results = append(results, delta)
			}
		case diff.PreDelta:
			if delta.(diff.PreDelta).PrePosition() == postion {
				results = append(results, delta)
			}
		default:
			panic("heh")
		}
	}
	return
}

const (
	AsciiSame    = " "
	AsciiAdded   = "+"
	AsciiDeleted = "-"
)

var AsciiStyles = map[string]string{
	AsciiAdded:   "diff-added",
	AsciiDeleted: "diff-deleted",
}

func (f *AsciiFormatter) push(name string, size int, array bool) {
	f.path = append(f.path, name)
	f.size = append(f.size, size)
	f.inArray = append(f.inArray, array)
}

func (f *AsciiFormatter) pop() {
	f.path = f.path[0 : len(f.path)-1]
	f.size = f.size[0 : len(f.size)-1]
	f.inArray = f.inArray[0 : len(f.inArray)-1]
}

func (f *AsciiFormatter) addLineWith(marker string, value string) {
	f.line = &AsciiLine{
		marker: marker,
		indent: len(f.path),
		buffer: bytes.NewBufferString(value),
	}
	f.closeLine()
}

func (f *AsciiFormatter) newLine(marker string) {
	f.line = &AsciiLine{
		marker: marker,
		indent: len(f.path),
		buffer: bytes.NewBuffer([]byte{}),
	}
}

func (f *AsciiFormatter) closeLine() {
	style, ok := AsciiStyles[f.line.marker]
	if f.config.Coloring && ok {
		f.buffer.WriteString(`<span class="` + style + `">`)
	}

	f.buffer.WriteString(f.line.marker)
	for n := 0; n < f.line.indent; n++ {
		f.buffer.WriteString("  ")
	}
	f.buffer.Write(f.line.buffer.Bytes())

	if f.config.Coloring && ok {
		f.buffer.WriteString(`</span>`)
	}

	// prob don't need
	f.buffer.WriteRune('\n')
}

func (f *AsciiFormatter) printKey(name string) {
	if !f.inArray[len(f.inArray)-1] {
		fmt.Fprintf(f.line.buffer, `"%s": `, name)
	} else if f.config.ShowArrayIndex {
		fmt.Fprintf(f.line.buffer, `%s: `, name)
	}
}

func (f *AsciiFormatter) printComma() {
	f.size[len(f.size)-1]--
	if f.size[len(f.size)-1] > 0 {
		f.line.buffer.WriteRune(',')
	}
}

func (f *AsciiFormatter) printValue(value interface{}) {
	switch value.(type) {
	case string:
		fmt.Fprintf(f.line.buffer, `"%s"`, value)
	case nil:
		f.line.buffer.WriteString("null")
	default:
		fmt.Fprintf(f.line.buffer, `%#v`, value)
	}
}

func (f *AsciiFormatter) print(a string) {
	f.line.buffer.WriteString(a)
}

func (f *AsciiFormatter) printRecursive(name string, value interface{}, marker string) {
	switch value.(type) {
	case map[string]interface{}:
		f.newLine(marker)
		f.printKey(name)
		f.print("{")
		f.closeLine()

		m := value.(map[string]interface{})
		size := len(m)
		f.push(name, size, false)

		keys := sortedKeys(m)
		for _, key := range keys {
			f.printRecursive(key, m[key], marker)
		}
		f.pop()

		f.newLine(marker)
		f.print("}")
		f.printComma()
		f.closeLine()

	case []interface{}:
		f.newLine(marker)
		f.printKey(name)
		f.print("[")
		f.closeLine()

		s := value.([]interface{})
		size := len(s)
		f.push("", size, true)
		for _, item := range s {
			f.printRecursive("", item, marker)
		}
		f.pop()

		f.newLine(marker)
		f.print("]")
		f.printComma()
		f.closeLine()

	default:
		f.newLine(marker)
		f.printKey(name)
		f.printValue(value)
		f.printComma()
		f.closeLine()
	}
}

func sortedKeys(m map[string]interface{}) (keys []string) {
	keys = make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return
}

// A Printer is responsible for outputting a diff.
type Printer struct {
	indentation int
	lines       int
	w           io.Writer
}

// NewPrinter creates a new printer, writing the output to the given
// writer.
func NewPrinter(w io.Writer) *Printer {
	return &Printer{
		indentation: 0,
		lines:       0,
		w:           w,
	}
}

// Format pretty prints a diff
//
// TODO(ben) this only goes one level deep
func (p *Printer) Format(left, right interface{}, deltas []diff.Delta) {
	// Reset the line count
	// lines := 0

	// omg thank you based defer
	p.indentation++
	defer func() {
		p.indentation--
	}()

	// Add missing fields from the right to the left so we can compute the `added` diffs
	left = mapMerge(left, right)

	// Invert the delta at this level
	deltaMap := deltaToMap(deltas)

	// Decide what to do based on type
	switch v := left.(type) {
	case map[string]interface{}:
		// lines++

		for k, obj := range v {
			if ds, ok := deltaMap[k]; ok {
				for _, d := range ds {
					fmt.Fprintf(p.w, `<div class="bg-light" style="margin: 0 .25rem">`)
					fmt.Fprintf(p.w, "<h3 class='indent-%d'>%s</h3>\n", p.indentation, k)

					// Get the change
					change := getChange(d)

					// If it _isn't_ a map (or an array eventually), then print it
					//
					// This might not be working because of the mapMerge behaviour adding the
					// `added` stuff at the wrong level
					if _, ok := obj.(map[string]interface{}); !ok {
						fmt.Fprintf(p.w, "<div class='indent-%d'>%s</div>\n", p.indentation+1, change)
					}
					fmt.Fprintf(p.w, `</div>`)
				}

				leftMap, ok := left.(map[string]interface{})
				if !ok {
					continue
				}

				rightMap, ok := right.(map[string]interface{})
				if !ok {
					continue
				}

				leftK, ok := leftMap[k]
				if !ok {
					continue
				}

				rightK, ok := rightMap[k]
				if !ok {
					continue
				}

				leftKMap, ok := leftK.(map[string]interface{})
				if !ok {
					continue
				}

				rightKMap, ok := rightK.(map[string]interface{})
				if !ok {
					continue
				}

				p.Format(leftKMap, rightKMap, ds)
			}
		}

	default:
	}
}

// mapMerge adds the missing key/value pairs from the right to the left maps.
//
// TODO(ben) support array elements as well
// TODO(ben) preserve overall ordering when adding keys
func mapMerge(left, right interface{}) interface{} {
	leftMap, ok := left.(map[string]interface{})
	if !ok {
		return left
	}

	rightMap, ok := right.(map[string]interface{})
	if !ok {
		return left
	}

	// Iterate over the right map. If a value exists in the right map that
	// _doesn't_ exist in the left map, add it.
	for k, v := range rightMap {
		if _, ok := leftMap[k]; !ok {
			leftMap[k] = v
		}
	}
	return left
}

// deltaToMap inverts a delta by a single level. It should be called every time the depth of a map changes
func deltaToMap(deltas []diff.Delta) map[string][]diff.Delta {
	invertedDelta := make(map[string][]diff.Delta)

	for _, delta := range deltas {
		switch delta.(type) {
		case *diff.Object:
			d := delta.(*diff.Object)
			invertedDelta[d.PostPosition().String()] = d.Deltas

		case *diff.Array:
			d := delta.(*diff.Array)
			invertedDelta[d.PostPosition().String()] = d.Deltas

		case *diff.Added:
			d := delta.(*diff.Added)
			invertedDelta[d.PostPosition().String()] = []diff.Delta{d}

		case *diff.Modified:
			d := delta.(*diff.Modified)
			invertedDelta[d.PostPosition().String()] = []diff.Delta{d}

		case *diff.TextDiff:
			d := delta.(*diff.TextDiff)
			invertedDelta[d.PostPosition().String()] = []diff.Delta{d}

		case *diff.Deleted:
			d := delta.(*diff.Deleted)
			invertedDelta[d.PrePosition().String()] = []diff.Delta{d}

		case *diff.Moved:
			d := delta.(*diff.Moved)
			invertedDelta[d.PostPosition().String()] = []diff.Delta{d}
		}
	}

	return invertedDelta
}

// getChange handles how to show the changes made.
//
// TODO(ben) abstract this enough so that it can render JSON, text, tables, etc
func getChange(delta diff.Delta) interface{} {
	switch delta.(type) {
	// could probably just return the object and arrays

	// TODO(ben) this is never called
	case *diff.Object:
		d := delta.(*diff.Object)
		for _, v := range d.Deltas {
			fmt.Println("anything happen")
			fmt.Printf("\t%v\n", getChange(v))
		}

	// case *diff.Array:
	// 	d := delta.(*diff.Array)

	case *diff.Added:
		d := delta.(*diff.Added)
		return fmt.Sprintf("<div class='circle circle-added'></div><strong>Added</strong> %v", d.Value)

	case *diff.Modified:
		d := delta.(*diff.Modified)
		return fmt.Sprintf("<div class='circle circle-changed'></div>%v → %v", d.OldValue, d.NewValue)

	case *diff.TextDiff:
		d := delta.(*diff.TextDiff)
		return fmt.Sprintf("<div class='circle circle-changed'></div>%v → %v", d.OldValue, d.NewValue)

	case *diff.Deleted:
		d := delta.(*diff.Deleted)
		return fmt.Sprintf("<div class='circle circle-deleted'></div><strong>Deleted</strong> %v", d.Value)

	case *diff.Moved:
		d := delta.(*diff.Moved)
		return fmt.Sprintf("<div class='circle circle-changed'></div><strong>Moved</strong> %v", d.Value)

	default:
		return nil
	}
	return nil
}
