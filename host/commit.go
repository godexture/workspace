package host

import "github.com/godexture/godec/access"

type outputRuntime struct {
	node            int
	outcome         int
	class           access.TransactionClass
	transaction     access.Transaction
	flusher         access.Flusher
	syncer          access.Syncer
	opened          bool
	commitAttempted bool
	committed       bool
}

func (r *runner) finishOutputs() *Failure {
	for _, output := range r.outputs {
		if output.flusher == nil {
			continue
		}
		node := r.result.Outputs[output.outcome].Node
		if failure := r.invoke(FlushPhase, node, "", output.flusher.Flush); failure != nil {
			return failure
		}
	}
	for _, output := range r.outputs {
		if output.syncer == nil {
			continue
		}
		node := r.result.Outputs[output.outcome].Node
		if failure := r.invoke(SyncPhase, node, "", output.syncer.Sync); failure != nil {
			return failure
		}
	}
	for _, output := range r.outputs {
		if output.transaction == nil {
			continue
		}
		node := r.result.Outputs[output.outcome].Node
		if failure := r.invoke(PrepareCommitPhase, node, "", output.transaction.PrepareCommit); failure != nil {
			return failure
		}
	}
	for _, output := range r.outputs {
		if output.transaction == nil {
			output.committed = true
			r.result.Outputs[output.outcome].State = OutputCommitted
			r.commitMetadataLosses(output.outcome)
			continue
		}
		node := r.result.Outputs[output.outcome].Node
		output.commitAttempted = true
		if failure := r.invoke(CommitPhase, node, "", output.transaction.Commit); failure != nil {
			r.result.Outputs[output.outcome].State = OutputUnknown
			return failure
		}
		output.committed = true
		r.result.Outputs[output.outcome].State = OutputCommitted
		r.commitMetadataLosses(output.outcome)
	}
	return nil
}

func (r *runner) commitMetadataLosses(outcome int) {
	if outcome < 0 || outcome >= len(r.result.Outputs) {
		return
	}
	output := &r.result.Outputs[outcome]
	if output.State != OutputCommitted || output.Choice < 0 {
		return
	}
	for _, value := range r.metadataLosses {
		if value.Output != output.Choice {
			continue
		}
		actual := ActualMetadataLoss{
			Output: value.Output, Node: value.Node, Component: value.Component, Port: value.Port, Report: value.Report,
		}
		output.MetadataLosses = append(output.MetadataLosses, actual)
		if actual.Lossy() {
			r.diag.metadataLoss(actual)
		}
	}
}
