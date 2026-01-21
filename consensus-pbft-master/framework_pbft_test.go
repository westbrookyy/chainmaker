/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"os/exec"
	"testing"
	"time"

	consensus_utils "chainmaker.org/chainmaker/consensus-utils/v2"
	"chainmaker.org/chainmaker/consensus-utils/v2/testframework"
	"chainmaker.org/chainmaker/logger/v2"
	"chainmaker.org/chainmaker/protocol/v2"
	"chainmaker.org/chainmaker/protocol/v2/test"
	consensuspb "chainmaker.org/chainmaker/pb-go/v2/consensus"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

var (
	blockchainId     = "chain1"
	nodeNums         = 4
	ConsensusEngines = make([]protocol.ConsensusEngine, nodeNums)
	CoreEngines      = make([]protocol.CoreEngine, nodeNums)
	consensusType    = consensuspb.ConsensusType_PBFT
)

// TestOnlyConsensus_PBFT 基础集成测试
// 测试PBFT共识在4节点网络中的基本功能
func TestOnlyConsensus_PBFT(t *testing.T) {
	// 清理测试环境
	cmd := exec.Command("/bin/sh", "-c", "rm -rf chain1 default.*")
	err := cmd.Run()
	require.Nil(t, err)

	// 初始化测试配置
	err = testframework.InitLocalConfigs()
	require.Nil(t, err)
	defer testframework.RemoveLocalConfigs()

	// 设置交易参数：交易大小200字节，交易数量10K
	testframework.SetTxSizeAndTxNum(200, 10*1024)
	testframework.InitLocalConfig(nodeNums)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 创建测试节点配置
	testNodeConfigs, err := testframework.CreateTestNodeConfig(
		ctrl, blockchainId, consensusType, nodeNums, nil, nil, nil)
	require.Nil(t, err)

	tLogger := logger.GetLogger(blockchainId)

	// 创建CoreEngine
	for i := 0; i < nodeNums; i++ {
		CoreEngines[i] = testframework.NewCoreEngineForTest(testNodeConfigs[i], tLogger)
	}

	// 创建PBFT共识引擎
	for i := 0; i < nodeNums; i++ {
		netService := testframework.NewNetServiceForTest()
		tc := &consensus_utils.ConsensusImplConfig{
			ChainId:     testNodeConfigs[i].ChainID,
			NodeId:      testNodeConfigs[i].NodeId,
			Ac:          testNodeConfigs[i].Ac,
			ChainConf:   testNodeConfigs[i].ChainConf,
			NetService:  netService,
			Core:        CoreEngines[i],
			Signer:      testNodeConfigs[i].Signer,
			LedgerCache: testNodeConfigs[i].LedgerCache,
			MsgBus:      testNodeConfigs[i].MsgBus,
			Logger:      newMockLogger(),
		}

		consensus, err := New(tc)
		require.Nil(t, err)
		ConsensusEngines[i] = consensus
	}

	// 创建测试集群框架
	tf, err := testframework.NewTestClusterFramework(
		blockchainId, consensusType, nodeNums, testNodeConfigs,
		ConsensusEngines, CoreEngines)
	require.Nil(t, err)

	// 设置日志
	l := &logger.LogConfig{
		SystemLog: logger.LogNodeConfig{
			FilePath:        "./default.log",
			LogLevelDefault: "DEBUG",
			LogLevels: map[string]string{
				"consensus": "DEBUG",
				"core":      "DEBUG",
				"net":       "DEBUG",
			},
			LogInConsole: false,
			ShowColor:    true,
		},
	}
	logger.SetLogConfig(l)

	// 启动测试
	tf.Start()
	defer tf.Stop()

	// 运行60秒
	time.Sleep(60 * time.Second)

	// 检查TPS
	cmd = exec.Command("/bin/sh", "-c", "cat default.*|grep TPS")
	out, err := cmd.CombinedOutput()
	require.Nil(t, err)
	require.NotNil(t, out)
}

// TestPBFT_BlockGenerationAndCommit 测试区块生成和提交
func TestPBFT_BlockGenerationAndCommit(t *testing.T) {
	// 清理测试环境
	cmd := exec.Command("/bin/sh", "-c", "rm -rf chain1 default.*")
	err := cmd.Run()
	require.Nil(t, err)

	err = testframework.InitLocalConfigs()
	require.Nil(t, err)
	defer testframework.RemoveLocalConfigs()

	testframework.SetTxSizeAndTxNum(200, 10*1024)
	testframework.InitLocalConfig(nodeNums)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	testNodeConfigs, err := testframework.CreateTestNodeConfig(
		ctrl, blockchainId, consensusType, nodeNums, nil, nil, nil)
	require.Nil(t, err)

	tLogger := logger.GetLogger(blockchainId)

	for i := 0; i < nodeNums; i++ {
		CoreEngines[i] = testframework.NewCoreEngineForTest(testNodeConfigs[i], tLogger)
	}

	for i := 0; i < nodeNums; i++ {
		netService := testframework.NewNetServiceForTest()
		tc := &consensus_utils.ConsensusImplConfig{
			ChainId:     testNodeConfigs[i].ChainID,
			NodeId:      testNodeConfigs[i].NodeId,
			Ac:          testNodeConfigs[i].Ac,
			ChainConf:   testNodeConfigs[i].ChainConf,
			NetService:  netService,
			Core:        CoreEngines[i],
			Signer:      testNodeConfigs[i].Signer,
			LedgerCache: testNodeConfigs[i].LedgerCache,
			MsgBus:      testNodeConfigs[i].MsgBus,
			Logger:      newMockLogger(),
		}

		consensus, err := New(tc)
		require.Nil(t, err)
		ConsensusEngines[i] = consensus
	}

	tf, err := testframework.NewTestClusterFramework(
		blockchainId, consensusType, nodeNums, testNodeConfigs,
		ConsensusEngines, CoreEngines)
	require.Nil(t, err)

	tf.Start()
	defer tf.Stop()

	// 等待区块生成
	time.Sleep(10 * time.Second)

	// 检查所有节点的区块高度
	for i := 0; i < nodeNums; i++ {
		height := ConsensusEngines[i].GetLastHeight()
		require.Greater(t, height, uint64(0), "节点 %d 应该有区块", i)
	}

	// 验证所有节点高度一致
	firstHeight := ConsensusEngines[0].GetLastHeight()
	for i := 1; i < nodeNums; i++ {
		require.Equal(t, firstHeight, ConsensusEngines[i].GetLastHeight(),
			"所有节点高度应该一致")
	}
}

func newMockLogger() protocol.Logger {
	return &test.GoLogger{}
}
