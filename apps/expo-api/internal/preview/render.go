package preview

import (
	"context"

	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
)

// FullRender is a preview's state after a full-file request: the leaf's
// preview phase (pending while a re-render runs) and whether the full file
// can be served. Ready without the full file means the re-render failed.
type FullRender struct {
	Status        progress.Status `json:"status" doc:"The preview's state: pending while a re-render runs."`
	FullAvailable bool            `json:"full_available" doc:"Whether the full-size preview file can be served now."`
}

// Renders asks for evicted full files to be rendered again.
type Renders struct {
	files   *Files
	requeue *Requeuer
	catalog leafProgress
}

func NewRenders(files *Files, requeue *Requeuer, catalog leafProgress) *Renders {
	return &Renders{files: files, requeue: requeue, catalog: catalog}
}

// RequestFull queues a preview-only re-render of blueprintID's preview
// (source sha, width x height) when its full file is missing, as the
// requester's work (ticket: the visitor who asked, not the entry's author;
// renderqueue.ErrFull past their cap). A failed re-render is not retried from
// here, so repeated requests cannot loop. queued reports whether a render was
// queued.
func (r *Renders) RequestFull(
	ctx context.Context, blueprintID, sha string, width, height int, ticket renderqueue.Ticket,
) (state FullRender, queued bool, err error) {
	state.FullAvailable = !previewimage.HasFull(width, height)
	if !state.FullAvailable {
		if state.FullAvailable, err = r.files.FullStored(ctx, sha, width, height); err != nil {
			return state, false, err
		}
	}
	if !state.FullAvailable {
		if queued, err = r.requeue.Requeue(ctx, blueprintID, progress.PhasePreview, false, ticket); err != nil {
			return state, false, err
		}
	}
	leaf, _, err := r.catalog.LeafProgress(ctx, blueprintID)
	state.Status = leaf.Preview.Status
	return state, queued, err
}
