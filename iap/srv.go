package iap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"
)

const (
	// SandboxURL is the endpoint for sandbox environment.
	SandboxURL string = "https://sandbox.itunes.apple.com/verifyReceipt"
	// ProductionURL is the endpoint for production environment.
	ProductionURL string = "https://buy.itunes.apple.com/verifyReceipt"
	// ContentType is the request content-type for apple store.
	ContentType string = "application/json; charset=utf-8"
)

// Client implements IAPClient
type Client struct {
	SandboxURL      string
	ProductionURL   string
	IsProductionEnv bool
	httpCli         *http.Client
}

// list of errore
var (
	ErrAppStoreServer = errors.New("AppStore server error")

	ErrInvalidJSON            = errors.New("The App Store could not read the JSON object you provided.")
	ErrInvalidReceiptData     = errors.New("The data in the receipt-data property was malformed or missing.")
	ErrReceiptUnauthenticated = errors.New("The receipt could not be authenticated.")
	ErrInvalidSharedSecret    = errors.New("The shared secret you provided does not match the shared secret on file for your account.")
	ErrServerUnavailable      = errors.New("The receipt server is not currently available.")
	ErrReceiptIsForTest       = errors.New("This receipt is from the test environment, but it was sent to the production environment for verification. Send it to the test environment instead.")
	ErrReceiptIsForProduction = errors.New("This receipt is from the production environment, but it was sent to the test environment for verification. Send it to the production environment instead.")
	ErrReceiptUnauthorized    = errors.New("This receipt could not be authorized. Treat this the same as if a purchase was never made.")

	ErrInternalDataAccessError = errors.New("Internal data access error.")
	ErrUnknown                 = errors.New("An unknown error occurred")
	ErrNotfound                = errors.New("transaction id not found")
)

// HandleError returns error message by status code
func HandleError(status int) error {
	var e error
	switch status {
	case 0:
		return nil
	case 21000:
		e = ErrInvalidJSON
	case 21002:
		e = ErrInvalidReceiptData
	case 21003:
		e = ErrReceiptUnauthenticated
	case 21004:
		e = ErrInvalidSharedSecret
	case 21005:
		e = ErrServerUnavailable
	case 21007:
		e = ErrReceiptIsForTest
	case 21008:
		e = ErrReceiptIsForProduction
	case 21009:
		e = ErrInternalDataAccessError
	case 21010:
		e = ErrReceiptUnauthorized
	default:
		if status >= 21100 && status <= 21199 {
			e = ErrInternalDataAccessError
		} else {
			e = ErrUnknown
		}
	}

	return fmt.Errorf("status %d: %w", status, e)
}

// New creates a client object
func New(IsProduction bool) *Client {
	client := &Client{
		SandboxURL:      SandboxURL,
		ProductionURL:   ProductionURL,
		IsProductionEnv: IsProduction,
		httpCli: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	return client
}

// NewWithClient creates a client with a custom http client.
func NewWithClient(client *http.Client, IsProduction bool) *Client {
	return &Client{
		IsProductionEnv: IsProduction,
		SandboxURL:      SandboxURL,
		ProductionURL:   ProductionURL,
		httpCli:         client,
	}
}

type Service interface {
	Verify(receipt, txid string) (*InApp, error)
}

func (c *Client) post(ctx context.Context, url string, reader io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, reader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", ContentType)
	req = req.WithContext(ctx)
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 500 {
		resp.Body.Close()
		return nil, fmt.Errorf("received http status code %d from the App Store: %w", resp.StatusCode, ErrAppStoreServer)
	}
	return resp, nil
}

// Verify sends receipts and gets validation result
func (c *Client) Verify(receipt, txid string) (*IAPResponse, error) {
	bts, err := json.Marshal(IAPRequest{
		ReceiptData: receipt})

	if err != nil {
		return nil, err
	}

	var req = bytes.NewBuffer(bts)
	ctx := context.Background()
	resp, err := c.post(ctx, c.ProductionURL, req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	var result = new(IAPResponse)
	req.Reset()
	req.Write(bts)
	if err = c.parseResponse(resp, result, ctx, req); err != nil {
		return nil, err
	}

	err = HandleError(result.Status)
	if err != nil {
		return nil, err
	}

	var ret []InApp
	for _, v := range result.Receipt.InApp {
		if v.TransactionID == txid {
			ret = append(ret, v)
		}
	}
	if len(ret) == 0 {
		return nil, ErrNotfound
	}
	result.Receipt.InApp = ret
	return result, nil
}

func (c *Client) parseResponse(resp *http.Response, result interface{}, ctx context.Context, body io.Reader) error {
	// Read the body now so that we can unmarshal it twice
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// https://developer.apple.com/library/content/technotes/tn2413/_index.html#//apple_ref/doc/uid/DTS40016228-CH1-RECEIPTURL
	var r StatusResponse
	err = json.Unmarshal(buf, &r)
	if err != nil {
		return err
	}

	if r.Status == 21007 && !c.IsProductionEnv {
		resp, err := c.post(ctx, c.SandboxURL, body)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		buf, err = ioutil.ReadAll(resp.Body)
	}

	err = json.Unmarshal(buf, &result)
	if err != nil {
		return err
	}
	return err
}
