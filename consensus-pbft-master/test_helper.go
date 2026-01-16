/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"chainmaker.org/chainmaker/logger/v2"
	"chainmaker.org/chainmaker/protocol/v2"
)

func newTestLogger() protocol.Logger {
	return logger.GetLoggerByChain(logger.MODULE_CONSENSUS, "test")
}
