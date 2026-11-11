// Package njiwamail is the Go client for the /v1 API.
package njiwamail

import "github.com/njiwa-tz/mail/client"

type Client = client.Client
type Email = client.Email
type Accepted = client.Accepted
type Status = client.Status
type APIError = client.APIError
type WebhookEvent = client.WebhookEvent

func New(baseURL, apiKey string) *Client {
	return &client.Client{BaseURL: baseURL, APIKey: apiKey}
}

var NewIdempotencyKey = client.NewIdempotencyKey
var VerifyWebhook = client.VerifyWebhook
