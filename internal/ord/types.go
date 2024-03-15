package ord

import verkle "github.com/ethereum/go-verkle"

type KeyValueMap = map[[32]byte][]byte

type State struct {
	Root   verkle.VerkleNode
	Height uint
	Hash   string

	KV KeyValueMap
}
