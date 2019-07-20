#!/bin/bash
for((i=1; i<=20; i++))
do
	go test -run=TestChallenge1Concurrent > $"out_"$i$".txt" && rm raftInfo.log raftWarn.log shardInfo.log kvInfo.log
done
