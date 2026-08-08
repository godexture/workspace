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
	}
	return nil
}
