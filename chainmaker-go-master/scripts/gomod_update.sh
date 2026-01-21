#!/usr/bin/env bash
#
# Copyright (C) BABEC. All rights reserved.
# Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
#
# SPDX-License-Identifier: Apache-2.0
#

set -x
BRANCH=$1
LWS_BRANCH='v1.2.1'
PRE_BRANCH="v2.3.4_qc"
LAST_PRE_BRANCH="v2.3.5_qc"
NET_COMMON_BRANCH='v1.2.8_qc'
NET_LIBP2P_BRANCH='v1.2.11_qc'
NET_LIQUID_BRANCH='v1.2.0_qc'
CHAINCONF_BRANCH='v2.3.5_qc'
COMMON_BRANCH='v2.3.8_qc'
LOCAL_CONF_BRANCH='v2.3.7_qc'
PROTOCOL_BRANCH='v2.3.9_qc'
PB_BRANCH='v2.3.6_qc'
UTILS_BRANCH='v2.3.5_qc'
LOGGER_BRANCH='v2.3.4_qc'
TBFT_BRANCH='v2.3.6_qc'
STORE_BRANCH='v2.3.6_qc'
VM_ENGINE_BRANCH='v2.3.6_qc'
EVM_BRANCH='v2.3.6_qc'

if [[ ! -n $BRANCH ]]; then
				  BRANCH="v2.3.7_qc"
fi
cd ..

go get chainmaker.org/chainmaker/lws@${LWS_BRANCH}
go get chainmaker.org/chainmaker/chainconf/v2@${CHAINCONF_BRANCH}
go get chainmaker.org/chainmaker/common/v2@${COMMON_BRANCH}
go get chainmaker.org/chainmaker/localconf/v2@${LOCAL_CONF_BRANCH}
go get chainmaker.org/chainmaker/logger/v2@${LOGGER_BRANCH}
go get chainmaker.org/chainmaker/pb-go/v2@${PB_BRANCH}
go get chainmaker.org/chainmaker/protocol/v2@${PROTOCOL_BRANCH}
go get chainmaker.org/chainmaker/utils/v2@${UTILS_BRANCH}
go get chainmaker.org/chainmaker/net-common@${NET_COMMON_BRANCH}
go get chainmaker.org/chainmaker/net-libp2p@${NET_LIBP2P_BRANCH}
go get chainmaker.org/chainmaker/net-liquid@${NET_LIQUID_BRANCH}
go get chainmaker.org/chainmaker/consensus-dpos/v2@${PRE_BRANCH}
go get chainmaker.org/chainmaker/consensus-raft/v2@${PRE_BRANCH}
go get chainmaker.org/chainmaker/consensus-solo/v2@${PRE_BRANCH}
go get chainmaker.org/chainmaker/consensus-utils/v2@${PRE_BRANCH}
go get chainmaker.org/chainmaker/consensus-maxbft/v2@${PRE_BRANCH}
go get chainmaker.org/chainmaker/consensus-tbft/v2@${TBFT_BRANCH}
go get chainmaker.org/chainmaker/store/v2@${STORE_BRANCH}
go get chainmaker.org/chainmaker/vm-docker-go/v2@${LAST_PRE_BRANCH}
go get chainmaker.org/chainmaker/vm-engine/v2@${VM_ENGINE_BRANCH}
go get chainmaker.org/chainmaker/vm-evm/v2@${EVM_BRANCH}
go get chainmaker.org/chainmaker/vm-gasm/v2@${LAST_PRE_BRANCH}
go get chainmaker.org/chainmaker/vm-native/v2@${BRANCH}
go get chainmaker.org/chainmaker/vm-wasmer/v2@${LAST_PRE_BRANCH}
go get chainmaker.org/chainmaker/vm-wxvm/v2@${LAST_PRE_BRANCH}
go get chainmaker.org/chainmaker/vm/v2@${BRANCH}
go get chainmaker.org/chainmaker/txpool-batch/v2@${PRE_BRANCH}
go get chainmaker.org/chainmaker/txpool-normal/v2@${PRE_BRANCH}
go get chainmaker.org/chainmaker/txpool-single/v2@${PRE_BRANCH}
go get chainmaker.org/chainmaker/sdk-go/v2@${BRANCH}

go mod tidy

make
make cmc
