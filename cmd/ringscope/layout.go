package main

import (
	"fmt"

	"github.com/ring-proto/ring/proto"
)

// gridLayout maps proto BuildGrid node ids ("g{x}-{y}") into a 2-D layout[y][x] for direct rendering;
// empty cells (none, on a full grid) read as no-node.
func gridLayout(ids []proto.NodeID, w, h int) [][]proto.NodeID {
	lay := make([][]proto.NodeID, h)
	for y := range lay {
		lay[y] = make([]proto.NodeID, w)
	}
	for _, id := range ids {
		var x, y int
		if _, err := fmt.Sscanf(string(id), "g%d-%d", &x, &y); err == nil && y >= 0 && y < h && x >= 0 && x < w {
			lay[y][x] = id
		}
	}
	return lay
}
