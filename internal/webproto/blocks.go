package webproto

import "github.com/amin-jalali/termocode/internal/blocks"

// The typed visual blocks of a chat message or a step (mobile plan 3.2)
// live in internal/blocks, a leaf package; these aliases keep the
// protocol's names in one place. Block.Validate holds the caps.
type (
	Block            = blocks.Block
	BlockItem        = blocks.BlockItem
	BlockColumn      = blocks.BlockColumn
	BlockGroup       = blocks.BlockGroup
	BlockDiff        = blocks.BlockDiff
	BlockHunk        = blocks.BlockHunk
	BlockDiffLine    = blocks.BlockDiffLine
	BlockField       = blocks.BlockField
	BlockBar         = blocks.BlockBar
	BlockFormField   = blocks.BlockFormField
	BlockBoard       = blocks.BlockBoard
	BlockBoardColumn = blocks.BlockBoardColumn
	BlockRef         = blocks.BlockRef
	BlockAction      = blocks.BlockAction
	ViewSpec         = blocks.ViewSpec
	ViewCond         = blocks.ViewCond
	ViewSort         = blocks.ViewSort
	MdNode           = blocks.MdNode
)

// MaxMsgBlocks is how many blocks one chat message or step carries.
const MaxMsgBlocks = 8
