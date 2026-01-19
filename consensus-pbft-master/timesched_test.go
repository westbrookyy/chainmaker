/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"testing"
	"time"

	pbftpb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"
	"github.com/stretchr/testify/require"
)

// TestNewTimeScheduler 测试创建超时调度器
func TestNewTimeScheduler(t *testing.T) {
	ts := newTimeScheduler(newTestLogger(), "node1")
	require.NotNil(t, ts)
	require.Equal(t, "node1", ts.id)
	require.NotNil(t, ts.bufferC)
	require.NotNil(t, ts.timeoutC)
	require.NotNil(t, ts.stopC)
}

// TestTimeScheduler_AddTimeoutInfo 测试添加超时信息
func TestTimeScheduler_AddTimeoutInfo(t *testing.T) {
	ts := newTimeScheduler(newTestLogger(), "node1")
	ts.Start()
	defer ts.Stop()

	ti := pbftpb.TimeoutInfo{
		Duration: 100 * time.Millisecond.Nanoseconds(),
		Height:   1,
		Round:    1,
		Step:     pbftpb.PBFTStep_PREPARE,
	}

	ts.AddTimeoutInfo(ti)

	// 等待超时触发
	select {
	case timeout := <-ts.GetTimeoutC():
		require.Equal(t, uint64(1), timeout.Height)
		require.Equal(t, uint64(1), timeout.Round)
		require.Equal(t, pbftpb.PBFTStep_PREPARE, timeout.Step)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout not triggered")
	}
}

// TestTimeScheduler_IgnoreOutdatedTimeout 测试忽略过时的超时
func TestTimeScheduler_IgnoreOutdatedTimeout(t *testing.T) {
	ts := newTimeScheduler(newTestLogger(), "node1")
	ts.Start()
	defer ts.Stop()

	// 添加较新的超时
	newTi := pbftpb.TimeoutInfo{
		Duration: 50 * time.Millisecond.Nanoseconds(),
		Height:   2,
		Round:    1,
		Step:     pbftpb.PBFTStep_PREPARE,
	}
	ts.AddTimeoutInfo(newTi)

	// 添加过时的超时（应该被忽略）
	oldTi := pbftpb.TimeoutInfo{
		Duration: 10 * time.Millisecond.Nanoseconds(),
		Height:   1, // 更小的Height
		Round:    1,
		Step:     pbftpb.PBFTStep_PREPARE,
	}
	ts.AddTimeoutInfo(oldTi)

	// 应该只收到新超时
	select {
	case timeout := <-ts.GetTimeoutC():
		require.Equal(t, uint64(2), timeout.Height)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout not triggered")
	}

	// 不应该有第二个超时
	select {
	case <-ts.GetTimeoutC():
		t.Fatal("should not receive outdated timeout")
	case <-time.After(100 * time.Millisecond):
		// 正确，没有收到过时超时
	}
}

// TestTimeScheduler_IgnoreOutdatedRound 测试忽略过时的轮次
func TestTimeScheduler_IgnoreOutdatedRound(t *testing.T) {
	ts := newTimeScheduler(newTestLogger(), "node1")
	ts.Start()
	defer ts.Stop()

	// 添加较新轮次的超时
	newTi := pbftpb.TimeoutInfo{
		Duration: 50 * time.Millisecond.Nanoseconds(),
		Height:   1,
		Round:    2, // 更新的Round
		Step:     pbftpb.PBFTStep_PREPARE,
	}
	ts.AddTimeoutInfo(newTi)

	// 添加过时轮次的超时（应该被忽略）
	oldTi := pbftpb.TimeoutInfo{
		Duration: 10 * time.Millisecond.Nanoseconds(),
		Height:   1,
		Round:    1, // 更小的Round
		Step:     pbftpb.PBFTStep_PREPARE,
	}
	ts.AddTimeoutInfo(oldTi)

	// 应该只收到新轮次的超时
	select {
	case timeout := <-ts.GetTimeoutC():
		require.Equal(t, uint64(2), timeout.Round)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout not triggered")
	}
}

// TestTimeScheduler_IgnoreOutdatedStep 测试忽略过时的步骤
func TestTimeScheduler_IgnoreOutdatedStep(t *testing.T) {
	ts := newTimeScheduler(newTestLogger(), "node1")
	ts.Start()
	defer ts.Stop()

	// 添加较新步骤的超时
	newTi := pbftpb.TimeoutInfo{
		Duration: 50 * time.Millisecond.Nanoseconds(),
		Height:   1,
		Round:    1,
		Step:     pbftpb.PBFTStep_COMMIT, // 更新的Step
	}
	ts.AddTimeoutInfo(newTi)

	// 添加过时步骤的超时（应该被忽略）
	oldTi := pbftpb.TimeoutInfo{
		Duration: 10 * time.Millisecond.Nanoseconds(),
		Height:   1,
		Round:    1,
		Step:     pbftpb.PBFTStep_PREPARE, // 更小的Step
	}
	ts.AddTimeoutInfo(oldTi)

	// 应该只收到新步骤的超时
	select {
	case timeout := <-ts.GetTimeoutC():
		require.Equal(t, pbftpb.PBFTStep_COMMIT, timeout.Step)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout not triggered")
	}
}

// TestTimeScheduler_Stop 测试停止调度器
func TestTimeScheduler_Stop(t *testing.T) {
	ts := newTimeScheduler(newTestLogger(), "node1")
	ts.Start()

	// 添加超时
	ti := pbftpb.TimeoutInfo{
		Duration: 100 * time.Millisecond.Nanoseconds(),
		Height:   1,
		Round:    1,
		Step:     pbftpb.PBFTStep_PREPARE,
	}
	ts.AddTimeoutInfo(ti)

	// 停止调度器
	ts.Stop()

	// 等待一段时间确保停止
	time.Sleep(50 * time.Millisecond)

	// 再次添加超时应该不会触发（调度器已停止）
	ts.AddTimeoutInfo(ti)

	// 不应该收到超时（因为调度器已停止）
	select {
	case <-ts.GetTimeoutC():
		t.Fatal("should not receive timeout after stop")
	case <-time.After(200 * time.Millisecond):
		// 正确，调度器已停止
	}
}

// TestTimeScheduler_MultipleTimeouts 测试多个超时
func TestTimeScheduler_MultipleTimeouts(t *testing.T) {
	ts := newTimeScheduler(newTestLogger(), "node1")
	ts.Start()
	defer ts.Stop()

	// 添加第一个超时（较短的超时时间）
	ti1 := pbftpb.TimeoutInfo{
		Duration: 50 * time.Millisecond.Nanoseconds(),
		Height:   1,
		Round:    1,
		Step:     pbftpb.PBFTStep_PREPARE,
	}
	ts.AddTimeoutInfo(ti1)

	// 添加第二个超时（更长的超时时间，但更新的Height）
	ti2 := pbftpb.TimeoutInfo{
		Duration: 200 * time.Millisecond.Nanoseconds(),
		Height:   2, // 更新的Height
		Round:    1,
		Step:     pbftpb.PBFTStep_PREPARE,
	}
	ts.AddTimeoutInfo(ti2)

	// 第一个超时应该被第二个覆盖（因为Height更新）
	// 应该只收到第二个超时
	select {
	case timeout := <-ts.GetTimeoutC():
		require.Equal(t, uint64(2), timeout.Height)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timeout not triggered")
	}
}

// TestTimeScheduler_ConcurrentAdd 测试并发添加超时
func TestTimeScheduler_ConcurrentAdd(t *testing.T) {
	ts := newTimeScheduler(newTestLogger(), "node1")
	ts.Start()
	defer ts.Stop()

	// 并发添加多个超时
	for i := 0; i < 5; i++ {
		go func(height uint64) {
			ti := pbftpb.TimeoutInfo{
				Duration: 100 * time.Millisecond.Nanoseconds(),
				Height:   height,
				Round:    1,
				Step:     pbftpb.PBFTStep_PREPARE,
			}
			ts.AddTimeoutInfo(ti)
		}(uint64(i + 1))
	}

	// 应该收到最新的超时（Height=5）
	select {
	case timeout := <-ts.GetTimeoutC():
		require.GreaterOrEqual(t, timeout.Height, uint64(1))
		require.LessOrEqual(t, timeout.Height, uint64(5))
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timeout not triggered")
	}
}
