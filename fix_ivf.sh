#!/bin/bash

# SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
# SPDX-License-Identifier: MIT

# Base directory for the dual video tracks
BASE_DIR="vnet/data/TestVnetRunnerDualVideoTracks/VariableAvailableCapacitySingleFlow/received_video"

# Input IVF files for both tracks (using new naming convention)
INPUT_IVF_TRACK1="${BASE_DIR}/received_0_track-1.ivf"
INPUT_IVF_TRACK2="${BASE_DIR}/received_0_track-2.ivf"

# Output MP4 files
OUTPUT_MP4_TRACK1="${BASE_DIR}/track1_output.mp4"
OUTPUT_MP4_TRACK2="${BASE_DIR}/track2_output.mp4"

# Function to process a single track
process_track() {
    local input_ivf="$1"
    local output_mp4="$2"
    local track_name="$3"

    echo "Processing $track_name..."

    # Create temp directory for this track
    local temp_dir="temp_frames_${track_name}"
    mkdir -p "$temp_dir"

    echo "Step 1: Extracting frames from $input_ivf..."
    # Extract frames without respecting timestamps (treating them as a sequence)
    ffmpeg -i "$input_ivf" -vsync 0 "$temp_dir/frame_%04d.png" 2>/dev/null

    # Count the number of frames
    local frame_count=$(ls "$temp_dir" 2>/dev/null | wc -l)
    echo "Extracted $frame_count frames for $track_name"

    if [ $frame_count -gt 0 ]; then
        echo "Step 2: Creating new video from frames at 30fps for $track_name..."
        # Create a new video from the frames at 30fps
        ffmpeg -framerate 30 -i "$temp_dir/frame_%04d.png" -c:v libx264 -pix_fmt yuv420p "$output_mp4" 2>/dev/null
        echo "$track_name output saved to $output_mp4"
    else
        echo "No frames found for $track_name in $input_ivf"
    fi

    # Remove temporary files
    rm -rf "$temp_dir"
}

echo "Processing dual video tracks..."

# Check if input files exist
if [ ! -f "$INPUT_IVF_TRACK1" ]; then
    echo "Track 1 file not found: $INPUT_IVF_TRACK1"
fi

if [ ! -f "$INPUT_IVF_TRACK2" ]; then
    echo "Track 2 file not found: $INPUT_IVF_TRACK2"
fi

# Process both tracks in parallel
if [ -f "$INPUT_IVF_TRACK1" ]; then
    process_track "$INPUT_IVF_TRACK1" "$OUTPUT_MP4_TRACK1" "track1" &
fi

if [ -f "$INPUT_IVF_TRACK2" ]; then
    process_track "$INPUT_IVF_TRACK2" "$OUTPUT_MP4_TRACK2" "track2" &
fi

# Wait for both processes to complete
wait

echo "Done! Both tracks processed:"
if [ -f "$OUTPUT_MP4_TRACK1" ]; then
    echo " Track 1: $OUTPUT_MP4_TRACK1"
fi
if [ -f "$OUTPUT_MP4_TRACK2" ]; then
    echo " Track 2: $OUTPUT_MP4_TRACK2"
fi