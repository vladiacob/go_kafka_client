/**
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 * 
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"time"
	"fmt"
	"os"
	"os/signal"
	"net"
	"strconv"
	kafkaClient "github.com/stealthly/go_kafka_client"
	metrics "github.com/rcrowley/go-metrics"
)

func resolveConfig() (*kafkaClient.ConsumerConfig, string, string, int, string, time.Duration) {
	rawConfig, err := kafkaClient.LoadConfiguration("consumers.properties")
	if err != nil {
		panic("Failed to load configuration file")
	}
	numConsumers, _ := strconv.Atoi(rawConfig["num_consumers"])
	zkTimeout, _ := time.ParseDuration(rawConfig["zookeeper_timeout"])

	numWorkers, _ := strconv.Atoi(rawConfig["num_workers"])
	maxWorkerRetries, _ := strconv.Atoi(rawConfig["max_worker_retries"])
	workerBackoff, _ := time.ParseDuration(rawConfig["worker_backoff"])
	workerRetryThreshold, _ := strconv.Atoi(rawConfig["worker_retry_threshold"])
	workerConsideredFailedTimeWindow, _ := time.ParseDuration(rawConfig["worker_considered_failed_time_window"])
	workerTaskTimeout, _ := time.ParseDuration(rawConfig["worker_task_timeout"])
	workerManagersStopTimeout, _ := time.ParseDuration(rawConfig["worker_managers_stop_timeout"])

	rebalanceMaxRetries, _ := strconv.Atoi(rawConfig["rebalance_max_retries"])
	rebalanceBackoff, _ := time.ParseDuration(rawConfig["rebalance_backoff"])
	partitionAssignmentStrategy, _ := rawConfig["partition_assignment_strategy"]
	excludeInternalTopics, _ := strconv.ParseBool(rawConfig["exclude_internal_topics"])

	numConsumerFetchers, _ := strconv.Atoi(rawConfig["num_consumer_fetchers"])
	fetchBatchSize, _ := strconv.Atoi(rawConfig["fetch_batch_size"])
	fetchMessageMaxBytes, _ := strconv.Atoi(rawConfig["fetch_message_max_bytes"])
	fetchMinBytes, _ := strconv.Atoi(rawConfig["fetch_min_bytes"])
	fetchBatchTimeout, _ := time.ParseDuration(rawConfig["fetch_batch_timeout"])
	requeueAskNextBackoff, _ := time.ParseDuration(rawConfig["requeue_ask_next_backoff"])
	fetchWaitMaxMs, _ := strconv.Atoi(rawConfig["fetch_wait_max_ms"])
	socketTimeout, _ := time.ParseDuration(rawConfig["socket_timeout"])
	queuedMaxMessages, _ := strconv.Atoi(rawConfig["queued_max_messages"])
	refreshLeaderBackoff, _ := time.ParseDuration(rawConfig["refresh_leader_backoff"])
	fetchMetadataRetries, _ := strconv.Atoi(rawConfig["fetch_metadata_retries"])
	fetchMetadataBackoff, _ := time.ParseDuration(rawConfig["fetch_metadata_backoff"])

	offsetsCommitMaxRetries, _ := strconv.Atoi(rawConfig["offsets_commit_max_retries"])

	flushInterval, _ := time.ParseDuration(rawConfig["flush_interval"])

	zkConfig := kafkaClient.NewZookeeperConfig()
	zkConfig.ZookeeperConnect = []string{rawConfig["zookeeper_connect"]}
	zkConfig.ZookeeperTimeout = zkTimeout
	return &kafkaClient.ConsumerConfig{
		Clientid: rawConfig["client_id"],
		Groupid: rawConfig["group_id"],
		NumWorkers: numWorkers,
		MaxWorkerRetries: maxWorkerRetries,
		WorkerBackoff: workerBackoff,
		WorkerRetryThreshold: int32(workerRetryThreshold),
		WorkerThresholdTimeWindow: workerConsideredFailedTimeWindow,
		WorkerTaskTimeout: workerTaskTimeout,
		WorkerManagersStopTimeout: workerManagersStopTimeout,
		RebalanceMaxRetries: int32(rebalanceMaxRetries),
		RebalanceBackoff: rebalanceBackoff,
		PartitionAssignmentStrategy: partitionAssignmentStrategy,
		ExcludeInternalTopics: excludeInternalTopics,
		NumConsumerFetchers: numConsumerFetchers,
		FetchBatchSize: fetchBatchSize,
		FetchMessageMaxBytes: int32(fetchMessageMaxBytes),
		FetchMinBytes: int32(fetchMinBytes),
		FetchBatchTimeout: fetchBatchTimeout,
		FetchTopicMetadataRetries: fetchMetadataRetries,
		FetchTopicMetadataBackoff: fetchMetadataBackoff,
		RequeueAskNextBackoff: requeueAskNextBackoff,
		FetchWaitMaxMs: int32(fetchWaitMaxMs),
		SocketTimeout: socketTimeout,
		QueuedMaxMessages: int32(queuedMaxMessages),
		RefreshLeaderBackoff: refreshLeaderBackoff,
		Coordinator: kafkaClient.NewZookeeperCoordinator(zkConfig),
		OffsetsStorage: rawConfig["offsets_storage"],
		AutoOffsetReset: rawConfig["auto_offset_reset"],
		OffsetsCommitMaxRetries: offsetsCommitMaxRetries,
	}, rawConfig["consumer_id_pattern"], rawConfig["topic"], numConsumers, rawConfig["graphite_connect"], flushInterval
}

func main() {
	config, consumerIdPattern, topic, numConsumers, graphiteConnect, graphiteFlushInterval := resolveConfig()
	startMetrics(graphiteConnect, graphiteFlushInterval)

	ctrlc := make(chan os.Signal, 1)
	signal.Notify(ctrlc, os.Interrupt)

	consumers := make([]*kafkaClient.Consumer, numConsumers)
	for i := 0; i < numConsumers; i++ {
		consumers[i] = startNewConsumer(*config, topic, consumerIdPattern, i)
		time.Sleep(10 * time.Second)
	}

	<-ctrlc
	fmt.Println("Shutdown triggered, closing all alive consumers")
	for _, consumer := range consumers {
		<-consumer.Close()
	}
	fmt.Println("Successfully shut down all consumers")
}

func startMetrics(graphiteConnect string, graphiteFlushInterval time.Duration) {
	addr, err := net.ResolveTCPAddr("tcp", graphiteConnect)
	if err != nil {
		panic(err)
	}
	go metrics.GraphiteWithConfig(metrics.GraphiteConfig{
		Addr:          addr,
		Registry:      metrics.DefaultRegistry,
		FlushInterval: graphiteFlushInterval,
		DurationUnit:  time.Second,
		Prefix:        "metrics",
		Percentiles:   []float64{0.5, 0.75, 0.95, 0.99, 0.999},
	})
}

func startNewConsumer(config kafkaClient.ConsumerConfig, topic string, consumerIdPattern string, consumerIndex int) *kafkaClient.Consumer {
	config.Consumerid = fmt.Sprintf(consumerIdPattern, consumerIndex)
	config.Strategy = GetStrategy(config.Consumerid)
	config.WorkerFailureCallback = FailedCallback
	config.WorkerFailedAttemptCallback = FailedAttemptCallback
	consumer := kafkaClient.NewConsumer(&config)
	topics := map[string]int {topic : config.NumConsumerFetchers}
	go func() {
		consumer.StartStatic(topics)
	}()
	return consumer
}

func GetStrategy(consumerId string) func(*kafkaClient.Worker, *kafkaClient.Message, kafkaClient.TaskId) kafkaClient.WorkerResult {
	consumeRate := metrics.NewRegisteredMeter(fmt.Sprintf("%s-ConsumeRate", consumerId), metrics.DefaultRegistry)
	return func(_ *kafkaClient.Worker, msg *kafkaClient.Message, id kafkaClient.TaskId) kafkaClient.WorkerResult {
		kafkaClient.Tracef("main", "Got a message: %s", string(msg.Value))
		consumeRate.Mark(1)

		return kafkaClient.NewSuccessfulResult(id)
	}
}

func FailedCallback(wm *kafkaClient.WorkerManager) kafkaClient.FailedDecision {
	kafkaClient.Info("main", "Failed callback")

	return kafkaClient.DoNotCommitOffsetAndStop
}

func FailedAttemptCallback(task *kafkaClient.Task, result kafkaClient.WorkerResult) kafkaClient.FailedDecision {
	kafkaClient.Info("main", "Failed attempt")

	return kafkaClient.CommitOffsetAndContinue
}
