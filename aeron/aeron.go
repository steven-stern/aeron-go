/*
Copyright 2016 Stanislav Liberman

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aeron

import (
	"github.com/lirm/aeron-go/aeron/broadcast"
	"github.com/lirm/aeron-go/aeron/counters"
	"github.com/lirm/aeron-go/aeron/driver"
	"github.com/lirm/aeron-go/aeron/ringbuffer"
	"github.com/lirm/aeron-go/aeron/util/memmap"
	"github.com/op/go-logging"
	"time"
)

// NewPublicationHandler is the handler type for new publication notification from the media driver
type NewPublicationHandler func(string, int32, int32, int64)

// NewSubscriptionHandler is the handler type for new subscription notification from the media driver
type NewSubscriptionHandler func(string, int32, int64)

// AvailableImageHandler is the handler type for image available notification from the media driver
type AvailableImageHandler func(*Image)

// UnavailableImageHandler is the handler type for image unavailable notification from the media driver
type UnavailableImageHandler func(*Image)

// Aeron is the primary interface to the media driver for managing subscriptions and publications
type Aeron struct {
	context            *Context
	conductor          ClientConductor
	toDriverRingBuffer rb.ManyToOne
	driverProxy        driver.Proxy

	counters *counters.MetaDataFlyweight
	cncFile  *memmap.File

	toClientsBroadcastReceiver *broadcast.Receiver
	toClientsCopyReceiver      *broadcast.CopyReceiver
}

var logger = logging.MustGetLogger("aeron")

// Connect is the factory method used to create a new instance of Aeron based on Context settings
func Connect(ctx *Context) *Aeron {
	aeron := new(Aeron)
	aeron.context = ctx
	logger.Debugf("Connecting with context: %v", ctx)

	aeron.counters, aeron.cncFile = counters.MapFile(ctx.CncFileName())

	aeron.toDriverRingBuffer.Init(aeron.counters.ToDriverBuf.Get())

	aeron.driverProxy.Init(&aeron.toDriverRingBuffer)

	aeron.toClientsBroadcastReceiver = broadcast.NewReceiver(aeron.counters.ToClientsBuf.Get())

	aeron.toClientsCopyReceiver = broadcast.NewCopyReceiver(aeron.toClientsBroadcastReceiver)

	clientLivenessTo := time.Duration(aeron.counters.ClientLivenessTo.Get())

	aeron.conductor.Init(&aeron.driverProxy, aeron.toClientsCopyReceiver, clientLivenessTo, ctx.mediaDriverTo,
		ctx.publicationConnectionTo, ctx.resourceLingerTo)
	aeron.conductor.counterValuesBuffer = aeron.counters.ValuesBuf.Get()

	aeron.conductor.onAvailableImageHandler = ctx.availableImageHandler
	aeron.conductor.onUnavailableImageHandler = ctx.unavailableImageHandler

	go aeron.conductor.Run(ctx.idleStrategy)

	return aeron
}

// Close will terminate client conductor and remove all publications and subscriptions from the media driver
func (aeron *Aeron) Close() error {
	err := aeron.conductor.Close()
	if nil != err {
		aeron.context.errorHandler(err)
	}

	err = aeron.cncFile.Close()
	if nil != err {
		aeron.context.errorHandler(err)
	}

	return err
}

func (aeron *Aeron) AddSubscription(channel string, streamID int32) chan *Subscription {
	ch := make(chan *Subscription, 1)

	regID := aeron.conductor.AddSubscription(channel, streamID)
	go func() {
		subscription := aeron.conductor.FindSubscription(regID)
		for subscription == nil {
			subscription = aeron.conductor.FindSubscription(regID)
			if subscription == nil {
				aeron.context.idleStrategy.Idle(0)
			}
		}
		ch <- subscription
		close(ch)
	}()

	return ch
}

func (aeron *Aeron) AddPublication(channel string, streamID int32) chan *Publication {
	ch := make(chan *Publication, 1)

	regID := aeron.conductor.AddPublication(channel, streamID)
	go func() {
		publication := aeron.conductor.FindPublication(regID)
		for publication == nil {
			publication = aeron.conductor.FindPublication(regID)
			if publication == nil {
				aeron.context.idleStrategy.Idle(0)
			}
		}
		ch <- publication
		close(ch)
	}()

	return ch
}
