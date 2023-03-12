package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/dstotijn/go-notion"
	"github.com/slack-go/slack"
)

type Response events.APIGatewayProxyResponse

func Handler(r events.APIGatewayProxyRequest) (Response, error) {
	if err := slackRequestVerifier(r); err != nil {
		log.Printf("[ERROR] failed to verify payload: %v", err)
		return Response{StatusCode: 200}, nil
	}
	log.Printf("[INFO] Done slackRequestVerifier")

	body, err := decodeBody(r.Body)
	if err != nil {
		log.Printf("[ERROR] Failed to decode base64 encoded payload: %v", err)
		return Response{StatusCode: 200}, nil
	}

	/*
		Slack sends a payload with a URL encoded body. We need to decode it first
		https://api.slack.com/apis/connections/events-api#the-events-api__receiving-events
	*/
	parameters, err := url.QueryUnescape(string(body)[8:])
	if err != nil {
		log.Printf("[ERROR] Failed to decode unescape message from slack: %v", err)
		return Response{StatusCode: 200}, nil
	}

	var message slack.AttachmentActionCallback
	if err := json.Unmarshal([]byte(parameters), &message); err != nil {
		log.Printf("[ERROR] Failed to decode json message from slack: %v", err)
		return Response{StatusCode: 200}, nil
	}

	title := message.View.State.Values["notion_title"]["title"].Value
	content := message.View.State.Values["notion_content"]["content"].Value

	if err := addPageToNotionDB(title, content); err != nil {
		log.Printf("[ERROR] Failed to add page to Notion: %v", err)
		return Response{StatusCode: 200}, nil
	}

	resp := Response{
		StatusCode:      200,
		IsBase64Encoded: false,
		Headers: map[string]string{
			"Content-Type":           "application/json",
			"X-MyCompany-Func-Reply": "slack-interaction-handler",
		},
	}

	return resp, nil
}

/*
	SlackRequestVerifier is a function that verifies requests from Slack.
	It validates the signature included in the Slack request to confirm that the request is from a legitimate source.
	The function takes an events.APIGatewayProxyRequest object as input and returns an error if the request fails verification.
*/
func slackRequestVerifier(r events.APIGatewayProxyRequest) error {
	header := http.Header{}
	for k, v := range r.Headers {
		header.Set(k, v)
	}

	sv, err := slack.NewSecretsVerifier(header, os.Getenv("SLACK_SIGNING_SECRET"))
	if err != nil {
		return err
	}

	sv.Write([]byte(r.Body))
	return sv.Ensure()
}

func decodeBody(body string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(body)
}

func makeHTTPHeader(headers map[string]string) http.Header {
	result := make(http.Header)
	for key, value := range headers {
		result.Add(key, value)
	}
	return result
}

func addPageToNotionDB(title string, content string) error {
	notionClient := notion.NewClient(os.Getenv("NOTION_TOKEN"))
	ctx := context.Background()
	notionTitle := []notion.RichText{
		{
			Type: notion.RichTextTypeText,
			Text: &notion.Text{Content: title},
		},
	}

	notionContext := []notion.RichText{
		{
			Type: notion.RichTextTypeText,
			Text: &notion.Text{Content: content},
		},
	}

	children := []notion.Block{
		{
			Object:    "block",
			Type:      notion.BlockTypeParagraph,
			Paragraph: &notion.RichTextBlock{Text: notionContext},
		},
	}
	properties := &notion.DatabasePageProperties{
		"Name": notion.DatabasePageProperty{Title: notionTitle},
	}

	params := notion.CreatePageParams{
		ParentID:               os.Getenv("NOTION_DATABASE"),
		ParentType:             notion.ParentTypeDatabase,
		Title:                  notionTitle,
		DatabasePageProperties: properties,
		Children:               children,
	}

	_, err := notionClient.CreatePage(ctx, params)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	lambda.Start(Handler)
}
