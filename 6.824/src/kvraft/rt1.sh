#!/bin/bash
for((i=1; i<=10; i++))
do
	rm $"out_"$i$".txt"
done
