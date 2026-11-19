// Package awsx is the Gateway's one seam to the AWS services the Essera integration uses:
// the configuration bucket, the configuration and usage queues, and Secrets Manager.
// Credentials come from the ECS task role through the default chain.
package awsx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

var (
	loadOnce sync.Once
	loaded   aws.Config
	loadErr  error
)

func awsConfig(ctx context.Context) (aws.Config, error) {
	loadOnce.Do(func() {
		loaded, loadErr = awsconfig.LoadDefaultConfig(ctx)
		if loadErr == nil && settings.AWSEndpoint() != "" {
			loaded.BaseEndpoint = aws.String(settings.AWSEndpoint())
		}
	})
	return loaded, loadErr
}

func s3Client(ctx context.Context) (*s3.Client, error) {
	cfg, err := awsConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = settings.AWSEndpoint() != "" }), nil
}

func sqsClient(ctx context.Context) (*sqs.Client, error) {
	cfg, err := awsConfig(ctx)
	if err != nil {
		return nil, err
	}
	return sqs.NewFromConfig(cfg), nil
}

// GetObject returns the object's bytes; found is false when it does not exist.
func GetObject(ctx context.Context, bucket, key string) (data []byte, found bool, err error) {
	client, err := s3Client(ctx)
	if err != nil {
		return nil, false, err
	}
	out, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		var missing *s3types.NoSuchKey
		if errors.As(err, &missing) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer out.Body.Close()
	data, err = io.ReadAll(out.Body)
	return data, err == nil, err
}

func PutJSON(ctx context.Context, bucket, key string, body []byte) error {
	client, err := s3Client(ctx)
	if err != nil {
		return err
	}
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/json"),
	})
	return err
}

func DeleteObject(ctx context.Context, bucket, key string) error {
	client, err := s3Client(ctx)
	if err != nil {
		return err
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	return err
}

// ListKeys lists the keys under a prefix, at most 1000.
func ListKeys(ctx context.Context, bucket, prefix string) ([]string, error) {
	client, err := s3Client(ctx)
	if err != nil {
		return nil, err
	}
	out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(out.Contents))
	for _, object := range out.Contents {
		keys = append(keys, aws.ToString(object.Key))
	}
	return keys, nil
}

func QueueURL(ctx context.Context, name string) (string, error) {
	client, err := sqsClient(ctx)
	if err != nil {
		return "", err
	}
	out, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.QueueUrl), nil
}

// Message is one received queue message.
type Message struct {
	Body          string
	ReceiptHandle string
}

// Receive long-polls the queue for up to 20 seconds.
func Receive(ctx context.Context, queueURL string, max int32) ([]Message, error) {
	client, err := sqsClient(ctx)
	if err != nil {
		return nil, err
	}
	out, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueURL),
		MaxNumberOfMessages: max,
		WaitTimeSeconds:     20,
	})
	if err != nil {
		return nil, err
	}
	messages := make([]Message, 0, len(out.Messages))
	for _, message := range out.Messages {
		messages = append(messages, Message{Body: aws.ToString(message.Body), ReceiptHandle: aws.ToString(message.ReceiptHandle)})
	}
	return messages, nil
}

func Delete(ctx context.Context, queueURL, receiptHandle string) error {
	client, err := sqsClient(ctx)
	if err != nil {
		return err
	}
	_, err = client.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(queueURL), ReceiptHandle: aws.String(receiptHandle)})
	return err
}

// BatchEntry is one message to send, identified within its batch by ID.
type BatchEntry struct {
	ID   string
	Body string
}

// SendBatch sends up to 10 messages and returns the ids SQS did not accept.
func SendBatch(ctx context.Context, queueURL string, entries []BatchEntry) (failed []string, err error) {
	client, err := sqsClient(ctx)
	if err != nil {
		return nil, err
	}
	request := make([]sqstypes.SendMessageBatchRequestEntry, 0, len(entries))
	for _, entry := range entries {
		request = append(request, sqstypes.SendMessageBatchRequestEntry{Id: aws.String(entry.ID), MessageBody: aws.String(entry.Body)})
	}
	out, err := client.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{QueueUrl: aws.String(queueURL), Entries: request})
	if err != nil {
		return nil, err
	}
	for _, failure := range out.Failed {
		failed = append(failed, aws.ToString(failure.Id))
	}
	return failed, nil
}

// SecretString reads a secret by name or ARN.
func SecretString(ctx context.Context, reference string) (string, error) {
	cfg, err := awsConfig(ctx)
	if err != nil {
		return "", err
	}
	client := secretsmanager.NewFromConfig(cfg)
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(reference)})
	if err != nil {
		return "", err
	}
	if out.SecretString == nil {
		return "", errors.New("the secret has no string value")
	}
	return *out.SecretString, nil
}

// Credentials are the task role's current credentials and the region they sign for.
func Credentials(ctx context.Context) (aws.Credentials, string, error) {
	cfg, err := awsConfig(ctx)
	if err != nil {
		return aws.Credentials{}, "", err
	}
	if cfg.Credentials == nil {
		return aws.Credentials{}, "", errors.New("no AWS credentials")
	}
	credentials, err := cfg.Credentials.Retrieve(ctx)
	return credentials, cfg.Region, err
}
