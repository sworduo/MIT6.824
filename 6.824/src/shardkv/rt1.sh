#!/bin/bash
rm raftInfo.log raftWarn.log shardInfo.log kvInfo.log
for((i=1; i<=0; i++))
do
	rm $"out_"$i$".txt"
done
