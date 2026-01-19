#!/bin/bash
# 优化版：使用cmc内置并发功能批量调用fact合约save方法

CMC_PATH="./cmc"                          # cmc工具路径
SDK_CONF="./testdata/sdk_config.yml"      # SDK配置文件路径
CONTRACT_NAME="fact"                      # 合约名
METHOD="save"                             # fact合约核心方法：保存存证
TOTAL_TX=10000                           # 总交易数
CONCURRENCY=1000                          # 并发数（goroutine数量）
SYNC_RESULT=false                         # 是否同步等待结果（建议设为false以提高性能）

# 计算每个goroutine需要发送的交易数
# 总交易数 = 并发数 * 每个goroutine的交易数
TOTAL_COUNT_PER_GOROUTINE=$((TOTAL_TX / CONCURRENCY))

# 如果无法整除，向上取整
if [ $((TOTAL_TX % CONCURRENCY)) -ne 0 ]; then
    TOTAL_COUNT_PER_GOROUTINE=$((TOTAL_COUNT_PER_GOROUTINE + 1))
fi

echo "=========================================="
echo "优化版批量发送fact合约交易"
echo "=========================================="
echo "总交易数: ${TOTAL_TX}"
echo "并发数(goroutine): ${CONCURRENCY}"
echo "每个goroutine交易数: ${TOTAL_COUNT_PER_GOROUTINE}"
echo "实际发送交易数: $((CONCURRENCY * TOTAL_COUNT_PER_GOROUTINE))"
echo "同步等待结果: ${SYNC_RESULT}"
echo "开始时间: $(date +%Y-%m-%d\ %H:%M:%S)"
echo "=========================================="

# 生成唯一参数（使用时间戳和随机数）
FILE_NAME="${CONTRACT_NAME}_${TOTAL_TX}_${CONCURRENCY}_$(date +%s)"
FILE_HASH=$(echo -n "${FILE_NAME}" | md5sum | cut -d' ' -f1)
TIME_STAMP=$(date +%s)

# 构造JSON参数
PARAMS="{\"file_name\":\"${FILE_NAME}\",\"file_hash\":\"${FILE_HASH}\",\"time\":\"${TIME_STAMP}\"}"

# 使用cmc内置并发功能发送交易
# 注意：cmc的并发是在Go层面实现的，比bash脚本的进程并发效率高得多
$CMC_PATH client contract user invoke \
    --contract-name="${CONTRACT_NAME}" \
    --method="${METHOD}" \
    --sdk-conf-path="${SDK_CONF}" \
    --params="${PARAMS}" \
    --concurrency="${CONCURRENCY}" \
    --total-count-per-goroutine="${TOTAL_COUNT_PER_GOROUTINE}" \
    --sync-result="${SYNC_RESULT}"

EXIT_CODE=$?

echo "=========================================="
echo "结束时间: $(date +%Y-%m-%d\ %H:%M:%S)"
if [ $EXIT_CODE -eq 0 ]; then
    echo "状态: 执行完成"
else
    echo "状态: 执行失败 (退出码: ${EXIT_CODE})"
fi
echo "=========================================="

exit $EXIT_CODE

