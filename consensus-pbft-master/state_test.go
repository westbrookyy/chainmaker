package pbft

import (
	"testing"

	pbftpb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"
	"github.com/stretchr/testify/require"
)

func TestConsensusStateCache(t *testing.T) {
	cache := newConsensusStateCache(2)

	state1 := &ConsensusState{Sequence: 1}
	state2 := &ConsensusState{Sequence: 2}
	state3 := &ConsensusState{Sequence: 3}

	cache.addConsensusState(state1)
	cache.addConsensusState(state2)
	require.Equal(t, state1, cache.getConsensusState(1))
	require.Equal(t, state2, cache.getConsensusState(2))

	cache.addConsensusState(state3)
	require.Nil(t, cache.getConsensusState(1)) // evicted by gc
	require.Equal(t, state2, cache.getConsensusState(2))
	require.Equal(t, state3, cache.getConsensusState(3))
}

func TestConsensusStateToProto(t *testing.T) {
	cs := &ConsensusState{
		Id:       "node1",
		View:     2,
		Sequence: 10,
		Step:     pbftpb.PBFTStep_PREPARE,
	}
	proto := cs.toProto()
	require.Equal(t, cs.Id, proto.Id)
	require.Equal(t, cs.View, proto.View)
	require.Equal(t, cs.Sequence, proto.Sequence)
	require.Equal(t, cs.Step, proto.Step)
}
