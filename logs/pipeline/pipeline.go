// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package pipeline

import (
	"context"

	logsconfig "flashcat.cloud/categraf/config/logs"
	"flashcat.cloud/categraf/logs/client"
	"flashcat.cloud/categraf/logs/client/http"
	"flashcat.cloud/categraf/logs/client/tcp"
	"flashcat.cloud/categraf/logs/diagnostic"
	"flashcat.cloud/categraf/logs/message"
	"flashcat.cloud/categraf/logs/processor"
	"flashcat.cloud/categraf/logs/sender"
)

// Pipeline processes and sends messages to the backend
type Pipeline struct {
	InputChan chan *message.Message
	processor *processor.Processor
	sender    *sender.Sender
}

// NewPipeline returns a new Pipeline
func NewPipeline(outputChan chan *message.Message, processingRules []*logsconfig.ProcessingRule, endpoints *logsconfig.Endpoints, destinationsContext *client.DestinationsContext, diagnosticMessageReceiver diagnostic.MessageReceiver, serverless bool) *Pipeline {
	var destinations *client.Destinations
	if endpoints.UseHTTP {
		main := http.NewDestination(endpoints.Main, http.JSONContentType, destinationsContext, endpoints.BatchMaxConcurrentSend)
		additionals := []client.Destination{}
		for _, endpoint := range endpoints.Additionals {
			additionals = append(additionals, http.NewDestination(endpoint, http.JSONContentType, destinationsContext, endpoints.BatchMaxConcurrentSend))
		}
		destinations = client.NewDestinations(main, additionals)
	} else {
		main := tcp.NewDestination(endpoints.Main, endpoints.UseProto, destinationsContext)
		additionals := []client.Destination{}
		for _, endpoint := range endpoints.Additionals {
			additionals = append(additionals, tcp.NewDestination(endpoint, endpoints.UseProto, destinationsContext))
		}
		destinations = client.NewDestinations(main, additionals)
	}

	senderChan := make(chan *message.Message, logsconfig.ChanSize)

	var strategy sender.Strategy
	if endpoints.UseHTTP || serverless {
		strategy = sender.NewBatchStrategy(sender.ArraySerializer, endpoints.BatchWait, endpoints.BatchMaxConcurrentSend, endpoints.BatchMaxSize, endpoints.BatchMaxContentSize, "logs")
	} else {
		strategy = sender.StreamStrategy
	}
	sender := sender.NewSender(senderChan, outputChan, destinations, strategy)

	var encoder processor.Encoder
	if serverless {
		encoder = processor.JSONServerlessEncoder
	} else if endpoints.UseHTTP {
		encoder = processor.JSONEncoder
	} else if endpoints.UseProto {
		encoder = processor.ProtoEncoder
	} else {
		encoder = processor.RawEncoder
	}

	inputChan := make(chan *message.Message, logsconfig.ChanSize)
	processor := processor.New(inputChan, senderChan, processingRules, encoder, diagnosticMessageReceiver)

	return &Pipeline{
		InputChan: inputChan,
		processor: processor,
		sender:    sender,
	}
}

// Start launches the pipeline
func (p *Pipeline) Start() {
	p.sender.Start()
	p.processor.Start()
}

// Stop stops the pipeline
func (p *Pipeline) Stop() {
	p.processor.Stop()
	p.sender.Stop()
}

// Flush flushes synchronously the processor and sender managed by this pipeline.
func (p *Pipeline) Flush(ctx context.Context) {
	p.processor.Flush(ctx) // flush messages in the processor into the sender
	p.sender.Flush(ctx)    // flush the sender
}
