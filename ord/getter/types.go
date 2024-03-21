package getter

import "github.com/RiemaLabs/indexer-committee/ord"

type OrdTransfer struct {
	ID            uint
	InscriptionID string
	BlockHeight   uint
	OldSatpoint   string
	NewSatpoint   string
	NewPkscript   ord.Pkscript
	NewWallet     ord.Wallet
	SentAsFee     bool
	Content       []byte
	ContentType   string
}

type OrdGetter interface {
	GetLatestBlockHeight() (uint, error)
	GetBlockHash(blockHeight uint) (string, error)
	GetOrdTransfers(blockHeight uint) ([]OrdTransfer, error)
}