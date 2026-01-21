/*
Copyright (C) BABEC. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package syncmode

import (
	"chainmaker.org/chainmaker-go/module/core/provider"
	"chainmaker.org/chainmaker-go/module/core/provider/conf"
	"chainmaker.org/chainmaker/protocol/v2"
)

const ConsensusTypePBFT = "PBFT"

var NilPBFTProvider provider.CoreProvider = (*pbftProvider)(nil)

type pbftProvider struct {
}

// 核心引擎创建
func (tp *pbftProvider) NewCoreEngine(config *conf.CoreEngineConfig) (protocol.CoreEngine, error) {
	return NewCoreEngine(config)
}
