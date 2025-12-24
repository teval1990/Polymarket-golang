# 下单示例

这个示例演示如何使用 Polymarket Go SDK 创建和提交订单。

## 环境变量

### 必需变量

- `PRIVATE_KEY`: 你的以太坊私钥（用于签名）
- `TOKEN_ID`: 条件代币资产ID（例如：从订单簿或市场信息中获取）

### 可选变量

- `CLOB_HOST`: CLOB API 主机地址（默认：`https://clob.polymarket.com`）
- `CHAIN_ID`: 链ID（默认：`137`，Polygon主网）
- `SIGNATURE_TYPE`: 签名类型（默认：`0`，EOA）
  - `0`: EOA（外部拥有账户）
  - `1`: Magic/Email 钱包
  - `2`: Browser 钱包代理
- `FUNDER`: 代理钱包地址（可选，用于代理钱包场景）

### API 凭证（二选一）

**方式1：使用环境变量（推荐）**
- `CLOB_API_KEY`: API密钥
- `CLOB_SECRET`: API密钥
- `CLOB_PASSPHRASE`: API密钥

**方式2：自动创建/派生**
- 如果未设置上述环境变量，程序会自动尝试派生或创建新的API密钥

## 限价订单

限价订单允许你指定买入或卖出的价格。

### 环境变量

- `ORDER_SIDE`: 订单方向，`BUY` 或 `SELL`（默认：`BUY`）
- `PRICE`: 订单价格（0-1之间的浮点数，例如：`0.5`）
- `SIZE`: 条件代币数量（例如：`10.0`）
- `NONCE`: 用于链上取消的nonce（可选，默认：`0`）
- `EXPIRATION`: 订单过期时间戳（可选，默认：30天后）
- `USE_CONVENIENCE`: 是否使用便捷方法（默认：`true`）
  - `true`: 使用 `CreateAndPostOrder`（一步完成）
  - `false`: 分步操作（先 `CreateOrder`，再 `PostOrder`）

### 示例

```bash
# 设置环境变量
export PRIVATE_KEY="your_private_key"
export TOKEN_ID="your_token_id"
export ORDER_SIDE="BUY"
export PRICE="0.5"
export SIZE="10.0"

# 运行程序
go run main.go
```

## 市价订单

市价订单会以当前市场价格立即执行。

### 环境变量

- `ORDER_TYPE`: 设置为 `MARKET` 以使用市价订单
- `ORDER_SIDE`: 订单方向，`BUY` 或 `SELL`（默认：`BUY`）
- `AMOUNT`: 
  - 如果是 `BUY`：美元金额（例如：`100.0` 表示买入价值100美元的份额）
  - 如果是 `SELL`：份额数量（例如：`10.0` 表示卖出10个份额）
- `MARKET_ORDER_TYPE`: 订单类型（默认：`GTC`）
  - `GTC`: Good Till Cancel（直到取消）
  - `FOK`: Fill Or Kill（全部成交或取消）
  - `FAK`: Fill And Kill（部分成交后取消剩余）
- `NONCE`: 用于链上取消的nonce（可选，默认：`0`）

### 示例

```bash
# 设置环境变量
export PRIVATE_KEY="your_private_key"
export TOKEN_ID="your_token_id"
export ORDER_TYPE="MARKET"
export ORDER_SIDE="BUY"
export AMOUNT="100.0"  # 买入价值100美元的份额
export MARKET_ORDER_TYPE="GTC"

# 运行程序
go run main.go
```

## 完整示例脚本

创建一个 `run.sh` 文件：

```bash
#!/bin/bash

# 基本配置
export PRIVATE_KEY="your_private_key"
export CHAIN_ID="137"
export CLOB_HOST="https://clob.polymarket.com"

# API凭证（如果已有）
export CLOB_API_KEY="your_api_key"
export CLOB_SECRET="your_secret"
export CLOB_PASSPHRASE="your_passphrase"

# 订单参数
export TOKEN_ID="your_token_id"
export ORDER_SIDE="BUY"
export PRICE="0.5"
export SIZE="10.0"

# 运行
go run main.go
```

然后运行：

```bash
chmod +x run.sh
./run.sh
```

## 原始订单模式（跳过价格舍入）

默认情况下，SDK 会从 API 获取市场的 `tick_size` 并对价格进行舍入。例如，如果市场的 tick_size 是 `0.01`，价格 `0.567` 会被舍入到 `0.57`。

如果你想跳过这个步骤，直接使用你指定的精确价格，可以在代码中使用 `RawOrder` 选项：

```go
// 使用 RawOrder 模式跳过 tick_size 获取和价格舍入
negRisk := false
options := &polymarket.PartialCreateOrderOptions{
    RawOrder: true,     // 跳过 tick_size 和舍入
    NegRisk:  &negRisk, // 可选，不提供时会自动获取
}

order, err := client.CreateOrder(orderArgs, options)
// 或者
result, err := client.CreateAndPostOrder(orderArgs, options)
```

### 对比示例

| 模式 | 输入价格 | tick_size=0.01 时的实际价格 |
|------|----------|---------------------------|
| 标准模式 | 0.567 | 0.57（舍入到2位小数） |
| 原始模式 | 0.567 | 0.567（保持原始值） |

**注意**：使用原始模式时，如果价格精度超出市场支持的 tick_size，订单可能会被交易所拒绝。

## 订单类型

`CreateAndPostOrder` 支持通过 `OrderType` 选项指定不同的订单类型：

```go
// FAK 订单（Fill And Kill）- 部分成交后取消剩余
orderType := polymarket.OrderTypeFAK
options := &polymarket.PartialCreateOrderOptions{
    OrderType: &orderType,
}
result, err := client.CreateAndPostOrder(orderArgs, options)

// 组合使用：RawOrder + FAK
orderType := polymarket.OrderTypeFAK
options := &polymarket.PartialCreateOrderOptions{
    RawOrder:  true,
    OrderType: &orderType,
}
result, err := client.CreateAndPostOrder(orderArgs, options)
```

### 订单类型说明

| 类型 | 说明 |
|------|------|
| `GTC` | Good Till Cancel - 直到取消（默认） |
| `FOK` | Fill Or Kill - 全部成交或取消 |
| `FAK` | Fill And Kill - 部分成交后取消剩余 |
| `GTD` | Good Till Date - 直到指定时间（需要设置 `Expiration`） |