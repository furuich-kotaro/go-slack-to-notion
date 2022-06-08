package main

import (
	"encoding/base64"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/slack-go/slack"
)

type Response events.APIGatewayProxyResponse

func Handler(r events.APIGatewayProxyRequest) (Response, error) {

	if err := verify(r); err != nil {
		log.Printf("[ERROR] failed to verify payload: %v", err)
		return Response{StatusCode: 200}, nil
	}

	payload := buildPayloadMap(r.Body)

	inputModal := createInputModal()
	slackClient := slack.New(os.Getenv("SLACK_TOKEN"))
	if _, err := slackClient.OpenView(payload["trigger_id"], *inputModal); err != nil {
		log.Printf("[ERROR] failed to open modal: %v", err)
		return Response{StatusCode: 200}, nil
	}

	return Response{StatusCode: 200}, nil
}

func buildPayloadMap(body string) map[string]string {
	dec, _ := base64.StdEncoding.DecodeString(body)
	parameters, _ := url.QueryUnescape(string(dec))

	payload := make(map[string]string)
	for _, s := range strings.Split(parameters, "&") {
		arr := strings.Split(s, "=")
		payload[arr[0]] = arr[1]
	}

	return payload
}

func verify(r events.APIGatewayProxyRequest) error {

	header := http.Header{}
	for k, v := range r.Headers {
		header.Set(k, v)
	}

	sv, err := slack.NewSecretsVerifier(header, os.Getenv("SLACK_SIGNING_SECRET"))
	if err != nil {
		return err
	}

	body, err := base64.StdEncoding.DecodeString(r.Body)
	if err != nil {
		return err
	}

	sv.Write(body)
	return sv.Ensure()
}

func createInputModal() *slack.ModalViewRequest {
	titleInputLabel := slack.NewTextBlockObject("plain_text", "タイトル", true, false)
	titleInputElement := slack.NewPlainTextInputBlockElement(titleInputLabel, "title")
	titleInput := slack.NewInputBlock("notion_title", titleInputLabel, titleInputElement)

	contextInputLabel := slack.NewTextBlockObject("plain_text", "タスク内容", true, false)
	contextInputElement := slack.NewPlainTextInputBlockElement(contextInputLabel, "context")
	contextInputElement.Multiline = true
	contextInput := slack.NewInputBlock("notion_context", contextInputLabel, contextInputElement)

	inputModal := &slack.ModalViewRequest{
		Type:   slack.ViewType("modal"),
		Title:  slack.NewTextBlockObject("plain_text", "NotionのDBに追加する", true, false),
		Blocks: slack.Blocks{BlockSet: []slack.Block{titleInput, contextInput}},
		Close:  slack.NewTextBlockObject("plain_text", "キャンセル", true, false),
		Submit: slack.NewTextBlockObject("plain_text", "追加", true, false),
	}

	return inputModal
}

func main() {
	lambda.Start(Handler)
}
