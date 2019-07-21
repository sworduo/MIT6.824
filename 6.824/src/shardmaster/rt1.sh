#!/bin/bash
rm raftInfo.log raftWarn.log shardInfo.log kvInfo.log
for((i=1; i<=30; i++))
do
	rm $"out_"$i$".txt"
done
