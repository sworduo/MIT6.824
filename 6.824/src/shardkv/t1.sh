#!/bin/bash
for((i=1; i<=15; i++))
do
	go test -run=TestStaticShards > $"out_"$i$".txt" && rm raftInfo.log raftWarn.log shardInfo.log kvInfo.log
done
