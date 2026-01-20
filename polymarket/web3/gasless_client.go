package web3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/teval1990/Polymarket-golang/polymarket"
)

// PolymarketGaslessWeb3Client Polymarket Web3客户端（无gas交易）
// 仅支持：
// - Poly代理钱包 (signature_type=1)
// - Safe/Gnosis钱包 (signature_type=2)
type PolymarketGaslessWeb3Client struct {
	*BaseWeb3Client
	signer       *polymarket.Signer
	relayConfig  RelayConfig
	builderCreds *polymarket.ApiCreds
	httpClient   *http.Client
}

// NewPolymarketGaslessWeb3Client 创建新的PolymarketGaslessWeb3Client
func NewPolymarketGaslessWeb3Client(
	privateKey string,
	signatureType SignatureType,
	builderCreds *polymarket.ApiCreds,
	chainID int64,
	rpcURL string,
) (*PolymarketGaslessWeb3Client, error) {
	if signatureType != SignatureTypePolyProxy && signatureType != SignatureTypeSafe {
		return nil, fmt.Errorf("PolymarketGaslessWeb3Client only supports signature_type=1 (Poly proxy wallets) and signature_type=2 (Safe wallets)")
	}

	base, err := NewBaseWeb3Client(privateKey, signatureType, chainID, rpcURL)
	if err != nil {
		return nil, err
	}

	signer, err := polymarket.NewSigner(privateKey, int(chainID))
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	return &PolymarketGaslessWeb3Client{
		BaseWeb3Client: base,
		signer:         signer,
		relayConfig:    DefaultRelayConfig,
		builderCreds:   builderCreds,
		httpClient:     &http.Client{},
	}, nil
}

// Execute 通过无gas中继执行交易
func (c *PolymarketGaslessWeb3Client) Execute(to common.Address, data []byte, operationName string, metadata string) (*TransactionReceipt, error) {
	var body *RelaySubmitRequest
	var err error

	switch c.signatureType {
	case SignatureTypePolyProxy:
		// add by teval 2026-01-21
		to = ProxyFactoryAddress
		body, err = c.buildProxyRelayTransaction(to, data, metadata)
	case SignatureTypeSafe:
		body, err = c.buildSafeRelayTransaction(to, data, metadata)
	default:
		return nil, fmt.Errorf("invalid signature_type: %d", c.signatureType)
	}
	if err != nil {
		return nil, err
	}

	// 获取headers
	headers, err := c.getRelayHeaders(body)
	if err != nil {
		return nil, err
	}

	// 提交到中继
	resp, err := c.submitToRelay(body, headers)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Gasless txn submitted: %s\n", resp.TransactionHash)
	fmt.Printf("Transaction ID: %s\n", resp.TransactionID)
	fmt.Printf("State: %s\n", resp.State)

	// 等待确认
	if resp.TransactionHash != "" {
		receipt, err := c.waitForReceipt(common.HexToHash(resp.TransactionHash))
		if err != nil {
			return nil, err
		}

		if receipt.Status == 1 {
			fmt.Printf("%s succeeded\n", operationName)
		} else {
			fmt.Printf("%s failed\n", operationName)
		}

		return receipt, nil
	}

	return nil, fmt.Errorf("no transaction hash in response: %+v", resp)
}

// getRelayNonce 获取中继nonce
func (c *PolymarketGaslessWeb3Client) getRelayNonce(walletType string) (int, error) {
	url := fmt.Sprintf("%s/nonce?address=%s&type=%s", c.relayConfig.RelayURL, c.GetBaseAddress().Hex(), walletType)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to get nonce: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("failed to get nonce: %s", string(body))
	}

	var result struct {
		Nonce string `json:"nonce"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode nonce response: %w", err)
	}

	nonce, err := strconv.Atoi(result.Nonce)
	if err != nil {
		return 0, fmt.Errorf("invalid nonce value: %w", err)
	}

	return nonce, nil
}

func (c *PolymarketGaslessWeb3Client) getRelay(walletType string) (string, int, error) {
	url := fmt.Sprintf("%s/relay-payload?address=%s&type=%s", c.relayConfig.RelayURL, c.GetBaseAddress().Hex(), walletType)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get nonce: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("failed to get nonce: %s", string(body))
	}

	var result struct {
		Address string `json:"address"`
		Nonce   string `json:"nonce"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("failed to decode nonce response: %w", err)
	}

	nonce, err := strconv.Atoi(result.Nonce)
	if err != nil {
		return "", 0, fmt.Errorf("invalid nonce value: %w", err)
	}

	return result.Address, nonce, nil
}

// buildProxyRelayTransaction 构建Proxy中继交易
func (c *PolymarketGaslessWeb3Client) buildProxyRelayTransaction(to common.Address, data []byte, metadata string) (*RelaySubmitRequest, error) {
	// modify by Teval 2026-01-21
	// proxyNonce, err := c.getRelayNonce("PROXY")
	relayAddress, proxyNonce, err := c.getRelay("PROXY")
	if err != nil {
		return nil, err
	}

	gasPrice := "0"
	relayerFee := "0"

	// 编码代理交易 - 使用正确的切片类型
	calls := []ProxyCall{
		{
			TypeCode: 1,
			To:       to,
			Value:    big.NewInt(0),
			Data:     data,
		},
	}
	proxyData, err := ProxyFactoryABI.Pack("proxy", calls)
	if err != nil {
		return nil, fmt.Errorf("failed to encode proxy transaction: %w", err)
	}
	encodedTxnHex := "0x" + common.Bytes2Hex(proxyData)

	// 估算gas
	var gasLimit string
	gas, err := c.client.EstimateGas(context.Background(), ethereum.CallMsg{
		From: c.GetBaseAddress(),
		To:   &c.ProxyFactoryAddress,
		Data: proxyData,
	})
	if err != nil {
		gasLimit = "10000000"
	} else {
		gasLimit = strconv.FormatUint(uint64(float64(gas)*1.3)+100000, 10)
	}

	// 创建签名结构
	structBytes := CreateProxyStruct(
		c.GetBaseAddress().Hex(),
		c.ProxyFactoryAddress.Hex(),
		encodedTxnHex,
		relayerFee,
		gasPrice,
		gasLimit,
		strconv.Itoa(proxyNonce),
		c.relayConfig.RelayHub,
		relayAddress, // c.relayConfig.RelayAddress,
	)

	structHash := Keccak256Hash(structBytes)

	// 签名（eth_sign风格）
	prefixedHash := crypto.Keccak256(append([]byte("\x19Ethereum Signed Message:\n32"), common.FromHex(structHash)...))
	sig, err := crypto.Sign(prefixedHash, c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}
	// 调整v值
	if sig[64] < 27 {
		sig[64] += 27
	}
	signature := "0x" + common.Bytes2Hex(sig)

	proxyWalletAddr, err := c.GetPolyProxyAddress(c.account)
	if err != nil {
		return nil, err
	}

	return &RelaySubmitRequest{
		Data:        encodedTxnHex,
		From:        c.GetBaseAddress().Hex(),
		Metadata:    metadata,
		Nonce:       strconv.Itoa(proxyNonce),
		ProxyWallet: proxyWalletAddr.Hex(),
		Signature:   signature,
		SignatureParams: map[string]interface{}{
			"gasPrice":   gasPrice,
			"gasLimit":   gasLimit,
			"relayerFee": relayerFee,
			"relayHub":   c.relayConfig.RelayHub,
			"relay":      c.relayConfig.RelayAddress,
		},
		To:   c.ProxyFactoryAddress.Hex(),
		Type: "PROXY",
	}, nil
}

// buildSafeRelayTransaction 构建Safe中继交易
func (c *PolymarketGaslessWeb3Client) buildSafeRelayTransaction(to common.Address, data []byte, metadata string) (*RelaySubmitRequest, error) {
	safeNonce, err := c.getRelayNonce("SAFE")
	if err != nil {
		return nil, err
	}

	// 获取Safe交易哈希
	txHash, err := c.getSafeTransactionHash(to, data, big.NewInt(int64(safeNonce)))
	if err != nil {
		return nil, fmt.Errorf("failed to get safe transaction hash: %w", err)
	}

	// 签名
	prefixedHash := crypto.Keccak256(append([]byte("\x19Ethereum Signed Message:\n32"), txHash...))
	sig, err := crypto.Sign(prefixedHash, c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	// 调整v值（Safe格式）
	switch sig[64] {
	case 0, 27:
		sig[64] = 0x1f
	case 1, 28:
		sig[64] = 0x20
	}
	signature := "0x" + common.Bytes2Hex(sig)

	safeProxyAddr, err := c.GetSafeProxyAddress(c.account)
	if err != nil {
		return nil, err
	}

	return &RelaySubmitRequest{
		Data:        "0x" + common.Bytes2Hex(data),
		From:        c.GetBaseAddress().Hex(),
		Metadata:    metadata,
		Nonce:       strconv.Itoa(safeNonce),
		ProxyWallet: safeProxyAddr.Hex(),
		Signature:   signature,
		SignatureParams: map[string]interface{}{
			"baseGas":        "0",
			"gasPrice":       "0",
			"gasToken":       AddressZero.Hex(),
			"operation":      "0",
			"refundReceiver": AddressZero.Hex(),
			"safeTxnGas":     "0",
		},
		To:   to.Hex(),
		Type: "SAFE",
	}, nil
}

// getSafeTransactionHash 获取Safe交易哈希
func (c *PolymarketGaslessWeb3Client) getSafeTransactionHash(to common.Address, data []byte, nonce *big.Int) ([]byte, error) {
	txHashData, err := SafeABI.Pack("getTransactionHash",
		to,
		big.NewInt(0),
		data,
		uint8(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		AddressZero,
		AddressZero,
		nonce,
	)
	if err != nil {
		return nil, err
	}

	result, err := c.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &c.Address,
		Data: txHashData,
	}, nil)
	if err != nil {
		return nil, err
	}

	var hash [32]byte
	if err := SafeABI.UnpackIntoInterface(&hash, "getTransactionHash", result); err != nil {
		return nil, err
	}

	return hash[:], nil
}

// getRelayHeaders 获取中继请求headers
func (c *PolymarketGaslessWeb3Client) getRelayHeaders(body *RelaySubmitRequest) (map[string]string, error) {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	if c.builderCreds == nil {
		// 使用签名服务器获取headers
		payload := map[string]interface{}{
			"method": "POST",
			"path":   "/submit",
			"body":   string(bodyJSON),
		}

		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Post(c.relayConfig.SignURL, "application/json", bytes.NewReader(payloadJSON))
		if err != nil {
			return nil, fmt.Errorf("failed to get headers from sign server: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("sign server error: %s", string(respBody))
		}

		var headers map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&headers); err != nil {
			return nil, fmt.Errorf("failed to decode headers: %w", err)
		}

		return headers, nil
	}

	// 使用本地 Builder 凭证创建headers（使用 POLY_BUILDER_* 头部）
	requestArgs := &polymarket.RequestArgs{
		Method:      "POST",
		RequestPath: "/submit",
		Body:        body,
	}

	headers, err := polymarket.CreateBuilderHeaders(c.builderCreds, requestArgs)
	if err != nil {
		return nil, err
	}

	return headers, nil
}

// submitToRelay 提交到中继
func (c *PolymarketGaslessWeb3Client) submitToRelay(body *RelaySubmitRequest, headers map[string]string) (*RelayResponse, error) {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/submit", c.relayConfig.RelayURL)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to submit to relay: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("relay error: %s", string(respBody))
	}

	var result RelayResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode relay response: %w", err)
	}

	return &result, nil
}

// waitForReceipt 等待交易回执
func (c *PolymarketGaslessWeb3Client) waitForReceipt(txHash common.Hash) (*TransactionReceipt, error) {
	for {
		receipt, err := c.client.TransactionReceipt(context.Background(), txHash)
		if err == nil {
			return FromEthReceipt(receipt, c.account), nil
		}
		// 继续等待...
	}
}

// SplitPosition 分割USDC为两个互补头寸
func (c *PolymarketGaslessWeb3Client) SplitPosition(conditionID common.Hash, amount float64, negRisk bool) (*TransactionReceipt, error) {
	amountInt := ToWei(amount, 6)

	var to common.Address
	var data []byte
	var err error

	if negRisk {
		to = NegRiskAdapterAddress
		data, err = NegRiskAdapterABI.Pack("splitPosition", c.USDCAddress, HashZero, conditionID, []*big.Int{big.NewInt(1), big.NewInt(2)}, amountInt)
	} else {
		to = c.ConditionalTokensAddress
		data, err = ConditionalTokensABI.Pack("splitPosition", c.USDCAddress, HashZero, conditionID, []*big.Int{big.NewInt(1), big.NewInt(2)}, amountInt)
	}
	if err != nil {
		return nil, err
	}

	return c.Execute(to, data, "Split Position", "split")
}

// MergePosition 合并两个互补头寸为USDC
func (c *PolymarketGaslessWeb3Client) MergePosition(conditionID common.Hash, amount float64, negRisk bool) (*TransactionReceipt, error) {
	amountInt := ToWei(amount, 6)

	var to common.Address
	var data []byte
	var err error

	if negRisk {
		to = NegRiskAdapterAddress
		data, err = NegRiskAdapterABI.Pack("mergePositions", c.USDCAddress, HashZero, conditionID, []*big.Int{big.NewInt(1), big.NewInt(2)}, amountInt)
	} else {
		to = c.ConditionalTokensAddress
		data, err = ConditionalTokensABI.Pack("mergePositions", c.USDCAddress, HashZero, conditionID, []*big.Int{big.NewInt(1), big.NewInt(2)}, amountInt)
	}
	if err != nil {
		return nil, err
	}

	return c.Execute(to, data, "Merge Position", "merge")
}

// RedeemPosition 赎回头寸为USDC
func (c *PolymarketGaslessWeb3Client) RedeemPosition(conditionID common.Hash, amounts []float64, negRisk bool) (*TransactionReceipt, error) {
	var to common.Address
	var data []byte
	var err error

	if negRisk {
		to = NegRiskAdapterAddress
		intAmounts := make([]*big.Int, len(amounts))
		for i, amt := range amounts {
			intAmounts[i] = ToWei(amt, 6)
		}
		data, err = NegRiskAdapterABI.Pack("redeemPositions", conditionID, intAmounts)
	} else {
		to = c.ConditionalTokensAddress
		data, err = ConditionalTokensABI.Pack("redeemPositions", c.USDCAddress, HashZero, conditionID, []*big.Int{big.NewInt(1), big.NewInt(2)})
	}
	if err != nil {
		return nil, err
	}

	return c.Execute(to, data, "Redeem Position", "redeem")
}

// ConvertPositions 转换NegRisk No头寸为Yes头寸和USDC
func (c *PolymarketGaslessWeb3Client) ConvertPositions(questionIDs []string, amount float64) (*TransactionReceipt, error) {
	amountInt := ToWei(amount, 6)
	negRiskMarketID := common.HexToHash(questionIDs[0][:len(questionIDs[0])-2] + "00")
	indexSet := big.NewInt(int64(GetIndexSet(questionIDs)))

	to := NegRiskAdapterAddress
	data, err := NegRiskAdapterABI.Pack("convertPositions", negRiskMarketID, indexSet, amountInt)
	if err != nil {
		return nil, err
	}

	return c.Execute(to, data, "Convert Positions", "convert")
}
