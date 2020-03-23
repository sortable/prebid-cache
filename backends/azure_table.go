package backends

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"context"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/valyala/fasthttp"
)

type AzureValue struct {
	ID           string `json:"id"`
	Value        string `json:"value"`
	PartitionKey string `json:"partition"`
}

type AzureTableBackend struct {
	Client  *fasthttp.Client
	Account string
	Key     string
	URI     string

	partitionKeyPool sync.Pool // Stores [8]byte instances where the first chars are [" and the last are "]
}

func NewAzureBackend(account string, key string) *AzureTableBackend {

	log.Debugf("New Azure Backend: Account %s Key %s", account, key)

	c := &AzureTableBackend{
		Account: account,
		Key:     key,
		Client: &fasthttp.Client{
			MaxIdleConnDuration: 30 * time.Second,
			DialDualStack:       true,
			WriteTimeout:        15 * time.Second,
			ReadTimeout:         15 * time.Second,
		},
		URI: fmt.Sprintf("https://%s.documents.azure.com", account),

		partitionKeyPool: sync.Pool{
			New: func() interface{} {
				buffer := [8]byte{}
				buffer[0] = '['
				buffer[1] = '"'
				buffer[6] = '"'
				buffer[7] = ']'
				return buffer
			},
		},
	}

	log.Infof("New Azure Client: %s", account)

	return c
}

func (c *AzureTableBackend) signReq(verb, resourceType, resourceLink, date string) string {

	strToSign := fmt.Sprintf("%s\n%s\n%s\n%s\n\n",
		strings.ToLower(verb),
		resourceType,
		resourceLink,
		strings.ToLower(date),
	)

	decodedKey, _ := base64.StdEncoding.DecodeString(c.Key)
	sha256 := hmac.New(sha256.New, []byte(decodedKey))
	sha256.Write([]byte(strToSign))

	signature := base64.StdEncoding.EncodeToString(sha256.Sum(nil))
	u := url.QueryEscape(fmt.Sprintf("type=master&ver=1.0&sig=%s", signature))

	return u
}

func formattedRequestTime() string {
	t := time.Now().UTC()
	return t.Format("Mon, 02 Jan 2006 15:04:05 GMT")
}

func (c *AzureTableBackend) Send(ctx context.Context, req *fasthttp.Request, resp *fasthttp.Response, resourceType string, resourceId string) error {
	date := formattedRequestTime()
	req.Header.Add("x-ms-date", date)
	req.Header.Add("x-ms-version", "2017-01-19")
	req.Header.Add("Authorization", c.signReq(string(req.Header.Method()), resourceType, resourceId, date))

	deadline, ok := ctx.Deadline()
	var err error = nil
	if ok {
		err = c.Client.DoDeadline(req, resp, deadline)
	} else {
		err = c.Client.Do(req, resp)
	}

	return err
}

func (c *AzureTableBackend) Get(ctx context.Context, key string) (string, error) {

	if key == "" {
		return "", fmt.Errorf("Invalid Key")
	}

	// Full key for the stupid gets
	resourceLink := fmt.Sprintf("/dbs/prebidcache/colls/cache/docs/%s", key)
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	var resp = fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.Header.SetMethod("GET")
	req.SetRequestURI(fmt.Sprintf("%s%s", c.URI, resourceLink))
	req.SetBodyString("")

	req.Header.Add("x-ms-documentdb-partitionkey", c.wrapForHeader(c.makePartitionKey(key)))
	err := c.Send(ctx, req, resp, "docs", resourceLink[1:])
	if err != nil {
		log.Debugf("Failed to make request")
		return "", err
	}

	av := AzureValue{}
	err = json.Unmarshal(resp.Body(), &av)
	if err != nil {
		log.Debugf("Failed to decode request body into JSON")
		return "", err
	}

	if av.Value == "" {
		log.Debugf("Response had empty value: %v", av)
		return "", fmt.Errorf("Key not found")
	}

	return av.Value, nil
}

func (c *AzureTableBackend) Put(ctx context.Context, key string, value string, ttlSeconds int) error {

	if key == "" {
		return fmt.Errorf("Invalid Key")
	}

	if value == "" {
		return fmt.Errorf("Invalid Value")
	}
	partitionKey := c.makePartitionKey(key)
	log.Debugf("POST partition key %s", partitionKey)
	av := AzureValue{
		ID:           key,
		Value:        value,
		PartitionKey: partitionKey,
	}

	b, err := json.Marshal(&av)
	if err != nil {
		return err
	}

	resourceLink := "/dbs/prebidcache/colls/cache/docs"

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	var resp = fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.Header.SetMethod("POST")
	req.SetRequestURI(fmt.Sprintf("%s%s", c.URI, resourceLink))
	req.SetBody(b)

	req.Header.Add("x-ms-documentdb-partitionkey", c.wrapForHeader(partitionKey))
	if err != nil {
		return err
	}
	err = c.Send(ctx, req, resp, "docs", "dbs/prebidcache/colls/cache")
	return err
}

func (c *AzureTableBackend) makePartitionKey(objectKey string) string {
	end := len(objectKey)
	if end > 4 {
		end = 4
	}
	return objectKey[0:end]
}

func (c *AzureTableBackend) wrapForHeader(partitionKey string) string {
	buffer := c.partitionKeyPool.Get().([8]byte)
	defer c.partitionKeyPool.Put(buffer)
	copy(buffer[2:6], partitionKey)
	return string(buffer[:])
}
