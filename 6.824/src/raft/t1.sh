#!/bin/bash
for((i=1; i<=32; i++))
do
	time go test > $"out_"$i$".txt" && rm info.log warn.log err.log
done
