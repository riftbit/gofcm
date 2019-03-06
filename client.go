package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/pquerna/ffjson/ffjson"
	"github.com/valyala/fasthttp"
)

const (
	// DefaultEndpoint contains endpoint URL of FCM service.
	DefaultEndpoint = "https://fcm.googleapis.com/fcm/send"

	// DefaultTimeout duration in second
	DefaultTimeout time.Duration = 30 * time.Second
)

var (
	// ErrInvalidAPIKey occurs if API key is not set.
	ErrInvalidAPIKey = errors.New("client API Key is invalid")
)

// Client abstracts the interaction between the application server and the
// FCM server via HTTP protocol. The developer must obtain an API key from the
// Google APIs Console page and pass it to the `Client` so that it can
// perform authorized requests on the application server's behalf.
// To send a message to one or more devices use the Client's Send.
//
// If the `HTTP` field is nil, a zeroed http.Client will be allocated and used
// to send messages.
type Client struct {
	apiKey   string
	client   *fasthttp.Client
	endpoint string
	timeout  time.Duration
}

// NewClient creates new Firebase Cloud Messaging Client based on API key and
// with default endpoint and http client.
func NewClient(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, ErrInvalidAPIKey
	}
	c := &Client{
		apiKey:   apiKey,
		endpoint: DefaultEndpoint,
		client:   &fasthttp.Client{},
		timeout:  DefaultTimeout,
	}
	for _, o := range opts {
		if err := o(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

// Send sends a message to the FCM server without retrying in case of service
// unavailability. A non-nil error is returned if a non-recoverable error
// occurs (i.e. if the response status is not "200 OK").
func (c *Client) Send(msg *Message) (*Response, []byte, error) {
	// validate
	if err := msg.Validate(); err != nil {
		return nil, nil, err
	}

	// marshal message
	data, err := ffjson.Marshal(msg)
	if err != nil {
		return nil, nil, err
	}

	return c.send(data)
}

// SendWithRetry sends a message to the FCM server with defined number of
// retrying in case of temporary error.
func (c *Client) SendWithRetry(msg *Message, retryAttempts int) (*Response, []byte, error) {
	// validate
	if err := msg.Validate(); err != nil {
		return nil, nil, err
	}
	// marshal message
	data, err := ffjson.Marshal(msg)
	if err != nil {
		return nil, nil, err
	}

	resp := new(Response)
	var body []byte
	err = retry(func() error {
		var er error
		resp, body, er = c.send(data)
		return er
	}, retryAttempts)
	if err != nil {
		return nil, nil, err
	}

	return resp, body, nil
}

// send sends a request.
func (c *Client) send(data []byte) (*Response, []byte, error) {
	// create request
	req, err := http.NewRequest("POST", c.endpoint, bytes.NewBuffer(data))
	if err != nil {
		return nil, nil, err
	}

	// request with timeout
	ctx, cancel := context.WithTimeout(context.TODO(), c.timeout)
	defer cancel()
	req = req.WithContext(ctx)

	// add headers
	req.Header.Add("Authorization", fmt.Sprintf("key=%s", c.apiKey))
	req.Header.Add("Content-Type", "application/json")

	// execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, connectionError(err.Error())
	}
	defer resp.Body.Close()

	// check response status
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusInternalServerError {
			return nil, serverError(fmt.Sprintf("%d error: %s", resp.StatusCode, resp.Status))
		}
		return nil, fmt.Errorf("%d error: %s", resp.StatusCode, resp.Status)
	}

	// build return
	response := new(Response)
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return nil, nil, err
	}

	return response, nil, nil
}