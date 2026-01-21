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

// TestConsensusStateCache_DeepCopy 测试状态缓存是否进行深拷贝
func TestConsensusStateCache_DeepCopy(t *testing.T) {
	cache := newConsensusStateCache(10)

	// 创建一个状态
	state := &ConsensusState{
		Id:       "node1",
		View:     1,
		Sequence: 1,
		Step:     pbftpb.PBFTStep_PREPARE,
	}

	// 添加到缓存
	cache.addConsensusState(state)

	// 修改原始状态
	state.View = 999
	state.Sequence = 999
	state.Step = pbftpb.PBFTStep_COMMIT

	// 获取缓存的状态
	cachedState := cache.getConsensusState(1)
	require.NotNil(t, cachedState)

	// 验证缓存的状态没有被修改（深拷贝）
	require.Equal(t, uint64(1), cachedState.View)      // 应该是1，不是999
	require.Equal(t, uint64(1), cachedState.Sequence)  // 应该是1，不是999
	require.Equal(t, pbftpb.PBFTStep_PREPARE, cachedState.Step) // 应该是PREPARE，不是COMMIT
}
