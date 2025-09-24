const TARGET_SAMPLE_RATE = 16000;

class PCMProcessor extends AudioWorkletProcessor {
    constructor(options) {
        super();
        this.inputSampleRate = sampleRate;
    }

    process(inputs) {
        const input = inputs[0];
        if (!input || input.length === 0) {
            return true;
        }

        const channelData = input[0];
        if (!channelData || channelData.length === 0) {
            return true;
        }

        const copy = new Float32Array(channelData.length);
        copy.set(channelData);

        this.port.postMessage({ type: "PCM", payload: copy.buffer, sampleRate: this.inputSampleRate }, [copy.buffer]);
        return true;
    }
}

registerProcessor("pcm-processor", PCMProcessor);
