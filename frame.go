package main

// The screen is one rounded frame of sections split by shared edges, the
// layout asgitlog introduced: the filter input (the edge over it, the top
// border, carries what the list is scoped to and its state), the main section
// (list and preview, split by a divider; its bottom edge carries the
// matches/total counter under the list, at its right end, and, while the
// preview overflows, its position on the right) and the foot. Neighbours
// share an edge, so no line is spent on a border of their own.
//
// The foot (helpfoot.go) is the help, or the context and the panel's key when
// the tool has a context: what the rest of the screen cannot say (asgitlog and
// asgotochanged: which repository and branch; asgotonotes: which group is
// being browsed; asgotosession: which directory). A title is not context.
//
//	╭────────────────── [vs main] ─╮  0
//	│ filter ❯                     │  1
//	├───────────┬──────────────────┤  mainY
//	│ list      │ preview          │  listY …
//	├──── 3/12 ─┴─────────── 8/40 ─┤
//	│ context           f1 options │
//	╰──────────────────────────────╯

import (
	"strings"
)

// mainY is the edge over the main section, listY the first list line and
// frameRows every line that is not the main section's content or the foot:
// the borders, the input and the edges.
const (
	statusY   = 0 // the top border, which carries the scope and the state
	mainY     = 2
	listY     = mainY + 1
	frameRows = 5
)

// frameHead is everything above the main section: the top border with the
// status, the input.
func frameHead(w int, status, input string) []string {
	return []string{hline(w, "╭", "╮", "", status), framed(w, input)}
}

// splitMain is the main section with a list of listW cells and a preview of
// prevW cells (padding included) side by side: the top edge, one line per
// list row, and the bottom edge carrying the counter under the list and pos
// under the preview. list must hold lines of exactly listW cells; preview lines are
// padded here.
func splitMain(list, preview []string, listW, prevW int, counter, pos string) []string {
	side := stDim.Render("│")
	out := make([]string, 0, len(list)+2)
	out = append(out, stDim.Render("├"+strings.Repeat("─", listW))+hline(prevW+2, "┬", "┤", "", ""))
	for i := range list {
		d := ""
		if i < len(preview) {
			d = preview[i]
		}
		out = append(out, side+list[i]+side+fit(" "+d, prevW)+side)
	}
	return append(out, hline(listW+1, "├", "", "", counter)+hline(prevW+2, "┴", "┤", "", pos))
}
