package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/godexture/core/node"
)

// Preload drains every declared preload-phase port to EOF, before Start
// (and thus the rest of the pipeline's run) ever begins.
func (n *FilterAdapter) Preload(ctx context.Context) error {
	ports := make([]string, 0)
	for id, phase := range n.phases {
		if phase == node.InputPhasePreload {
			ports = append(ports, id)
		}
	}
	if len(ports) == 0 {
		return nil
	}
	sort.Strings(ports)
	aux, ok := n.engine.(AuxInputEngine)
	if !ok {
		return fmt.Errorf("filter has a preload input port but its engine does not implement AuxInputEngine")
	}
	for _, id := range ports {
		edge := n.inputs[id].Edge()
		if edge == nil {
			return fmt.Errorf("preload filter port %q not connected", id)
		}
		for {
			frame, err := edge.Pull(ctx)
			if errors.Is(err, io.EOF) {
				if err := aux.EndInput(id); err != nil {
					return err
				}
				break
			}
			if err != nil {
				return err
			}
			if err := aux.SendInput(id, &frame); err != nil {
				frame.Release()
				return err
			}
			frame.Release()
		}
	}
	return nil
}
