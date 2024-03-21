package stateless

import (
	"encoding/base64"
	"fmt"
	"log"

	"github.com/RiemaLabs/indexer-committee/ord"
	"github.com/RiemaLabs/indexer-committee/ord/getter"
	verkle "github.com/ethereum/go-verkle"
)

func (state DiffState) Copy() DiffState {
	newElements := make([]TripleElement, len(state.Diff.Elements))

	for i, elem := range state.Diff.Elements {
		newElements[i] = TripleElement{
			Key:      elem.Key,
			OldValue: elem.OldValue,
			NewValue: elem.NewValue,
		}
	}

	newDiff := DiffList{Elements: newElements}
	return DiffState{
		Height:       state.Height,
		Hash:         state.Hash,
		Diff:         newDiff,
		VerkleCommit: state.VerkleCommit,
	}
}

func (queue *Queue) StartHeight() uint {
	return queue.History[0].Height
}

func (queue *Queue) LatestHeight() uint {
	return queue.Header.Height
}

func (queue *Queue) Println() {
	log.Println("====", queue.Header.Height, "====", queue.Header.Hash, "====")
	for _, node := range queue.History {
		log.Print(node.Height, "*", node.Hash)
	}
}

func (queue *Queue) Update(getter getter.OrdGetter, latestHeight uint) error {
	curHeight := queue.Header.Height
	for i := curHeight + 1; i <= latestHeight; i++ {
		ordTransfer, err := getter.GetOrdTransfers(i)
		if err != nil {
			return err
		}
		// Write to Diff
		Exec(queue.Header, ordTransfer, i)
		hash, err := getter.GetBlockHash(i - 1)
		if err != nil {
			return err
		}
		newDiffState := DiffState{
			Height:       i - 1,
			Hash:         hash,
			Diff:         queue.Header.Diff,
			VerkleCommit: queue.Header.Root.Commit().Bytes(),
		}
		copy(queue.History[:], queue.History[1:])
		queue.History[len(queue.History)-1] = newDiffState

		queue.Header.OrdTrans = ordTransfer
		// header.Height ++
		queue.Header.Paging(getter, true, NodeResolveFn)
	}
	return nil
}

func Rollingback(stateDiff *DiffState) (verkle.VerkleNode, [][]byte, []TripleElement) {
	rollback := verkle.New()
	var keys [][]byte

	for _, elem := range stateDiff.Diff.Elements {
		keys = append(keys, elem.Key[:])
		if elem.OldValueExists {
			rollback.Insert(elem.Key[:], elem.OldValue[:], NodeResolveFn)
		}
	}

	return rollback, keys, stateDiff.Diff.Elements
}

func (queue *Queue) Recovery(getter getter.OrdGetter, reorgHeight uint) error {
	curHeight := queue.Header.Height
	startHeight := queue.StartHeight()

	// Rollback to the reorgHeight - 1.
	for i := curHeight - 1; i >= reorgHeight-1; i-- {
		index := i - startHeight
		pastState := queue.History[index]

		// Inner bug in go-verkle, doesn't work.
		// for _, elem := range pastState.Diff.Elements {
		// 	if elem.OldValueExists {
		// 		queue.Header.KV[elem.Key] = elem.OldValue
		// 		queue.Header.Root.Insert(elem.Key[:], elem.OldValue[:], NodeResolveFn)
		// 	} else {
		// 		delete(queue.Header.KV, elem.Key)
		// 		queue.Header.Root.Delete(elem.Key[:], NodeResolveFn)
		// 	}
		// }
		// newRoot := queue.Header.Root
		// newBytes := queue.Header.Root.Commit().Bytes()
		// n := base64.StdEncoding.EncodeToString(newBytes[:])

		for _, elem := range pastState.Diff.Elements {
			if elem.OldValueExists {
				queue.Header.KV[elem.Key] = elem.OldValue
			} else {
				delete(queue.Header.KV, elem.Key)
			}
		}
		newRoot := verkle.New()
		for k, v := range queue.Header.KV {
			newRoot.Insert(k[:], v[:], NodeResolveFn)
		}
		newBytes := newRoot.Commit().Bytes()
		n := base64.StdEncoding.EncodeToString(newBytes[:])
		o := base64.StdEncoding.EncodeToString(pastState.VerkleCommit[:])
		if n != o {
			panic(fmt.Sprintf("Recovery the header failed! The commitment is different: %s and %s", n, o))
		}
		newHeader := Header{
			Root:     newRoot,
			KV:       queue.Header.KV,
			Height:   i,
			Hash:     pastState.Hash,
			Diff:     DiffList{},
			TempKV:   KeyValueMap{},
			OrdTrans: queue.Header.OrdTrans,
		}
		queue.Header = &newHeader
	}

	// Compute to the curHeight from the reorgHeight.
	for i := reorgHeight; i <= curHeight; i++ {
		index := i - startHeight - 1
		ordTransfer, err := getter.GetOrdTransfers(i)
		if err != nil {
			return err
		}
		Exec(queue.Header, ordTransfer, i)
		var hash string
		hash, err = getter.GetBlockHash(i - 1)
		if err != nil {
			return err
		}
		queue.History[index] = DiffState{
			Height:       i - 1,
			Hash:         hash,
			Diff:         queue.Header.Diff,
			VerkleCommit: queue.Header.Root.Commit().Bytes(),
		}
		queue.Header.OrdTrans = ordTransfer
		queue.Header.Paging(getter, true, NodeResolveFn)
	}

	return nil
}

func (queue *Queue) CheckForReorg(getter getter.OrdGetter) (uint, error) {
	// return the height that needs to start reorg
	for i := 0; i <= len(queue.History)-1; i++ {
		state := queue.History[i]
		height := state.Height
		hash := state.Hash
		newHash, err := getter.GetBlockHash(height)
		if err != nil {
			return 0, err
		}
		if hash == newHash {
			continue
		} else {
			return height, nil
		}
	}
	return 0, nil
}

func NewQueues(getter getter.OrdGetter, header *Header, queryHash bool, startHeight uint) (*Queue, error) {
	var stateList [ord.BitcoinConfirmations]DiffState
	for i := startHeight; i <= startHeight+ord.BitcoinConfirmations-1; i++ {
		ordTransfer, err := getter.GetOrdTransfers(i)
		if err != nil {
			return nil, err
		}
		Exec(header, ordTransfer, i)
		var hash string
		if queryHash {
			hash, err = getter.GetBlockHash(i - 1)
			if err != nil {
				return nil, err
			}
		}
		stateList[i-startHeight] = DiffState{
			Height:       i - 1,
			Hash:         hash,
			Diff:         header.Diff,
			VerkleCommit: header.Root.Commit().Bytes(),
		}
		header.Paging(getter, true, NodeResolveFn)
	}
	queue := Queue{
		Header:  header,
		History: stateList,
	}
	return &queue, nil
}