#!/bin/bash
for((i=1; i<=10; i++))
do
	go test > $"out_"$i$".txt" && rm raftInfo.log raftWarn.log shardInfo.log kvInfo.log
done
