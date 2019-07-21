#!/bin/bash
for((i=1; i<=30; i++))
do
	go test -race > $"out_"$i$".txt"  && rm raftInfo.log raftWarn.log shardInfo.log kvInfo.log
done
