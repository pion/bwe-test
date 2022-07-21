set -x

OUT_DIR="./out"
IMPLEMENTATIONS=(TestVnetRunnerABR TestVnetRunnerSimulcast)

for i in "${IMPLEMENTATIONS[@]}";
do 
	RUN="$i/VariableAvailableCapacitySingleFlow"
	DIR="./vnet/data/$i/VariableAvailableCapacitySingleFlow"
	OUT="$OUT_DIR/$RUN"
	if [ -d "$DIR" ]
	then
		# TODO: Add capacity and config (for correct base timestamp)
		mkdir -p $OUT
		./plot.py --capacity $DIR/capacity.log --rtp-received $DIR/0_receiver_inbound_rtp.log --rtp-sent $DIR/0_sender_outbound_rtp.log --cc $DIR/0_cc.log --rtcp-received $DIR/0_sender_inbound_rtcp.log --rtcp-sent $DIR/0_receiver_outbound_rtcp.log -o $OUT/rates.png &
		./plot.py --loss $DIR/0_sender_outbound_rtp.log $DIR/0_receiver_inbound_rtp.log -o $OUT/loss.png &
		./plot.py --latency $DIR/0_sender_outbound_rtp.log $DIR/0_receiver_inbound_rtp.log -o $OUT/latency.png &
	fi
done

wait

