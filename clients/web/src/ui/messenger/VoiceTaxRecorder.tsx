"use client";

import { useRef, useState } from "react";
import { ControlRoomIcon } from "./ControlRoomIcon";

interface VoiceTaxRecorderProps {
  onReady: (original: File, taxed: File, durationMillis: number, taxLevel: string) => Promise<void>;
}

export function VoiceTaxRecorder({ onReady }: VoiceTaxRecorderProps) {
  const recorder = useRef<MediaRecorder | undefined>(undefined);
  const stream = useRef<MediaStream | undefined>(undefined);
  const chunks = useRef<Blob[]>([]);
  const maximumDuration = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const [recording, setRecording] = useState(false);
  const [processing, setProcessing] = useState(false);

  const start = async () => {
    if (!navigator.mediaDevices?.getUserMedia || !globalThis.MediaRecorder) {
      return;
    }
    const value = await navigator.mediaDevices.getUserMedia({ audio: true });
    stream.current = value;
    chunks.current = [];
    const next = new MediaRecorder(value);
    next.ondataavailable = (event) => chunks.current.push(event.data);
    next.onstop = () => void process();
    next.start();
    recorder.current = next;
    maximumDuration.current = setTimeout(stop, 10 * 60_000);
    setRecording(true);
  };

  const stop = () => {
    recorder.current?.stop();
    if (maximumDuration.current) clearTimeout(maximumDuration.current);
    stream.current?.getTracks().forEach((track) => track.stop());
    setRecording(false);
  };

  const process = async () => {
    setProcessing(true);
    try {
      const source = new Blob(chunks.current, { type: recorder.current?.mimeType || "audio/webm" });
      const context = new AudioContext();
      const decoded = await context.decodeAudioData(await source.arrayBuffer());
      await context.close();
      const durationMillis = Math.round(decoded.duration * 1000);
      const tax = taxProfile(durationMillis);
      const taxed = await renderTaxedAudio(decoded, tax.sampleRate, tax.frequency);
      await onReady(
        new File([source], "voice-original.webm", { type: source.type }),
        new File([taxed], "voice-taxed.wav", { type: "audio/wav" }),
        durationMillis,
        tax.level,
      );
    } finally {
      setProcessing(false);
    }
  };

  return <button aria-label={recording ? "Stop public voice recording" : "Record a publicly stored voice note"} className={`attach-button ${recording ? "recording" : ""}`} disabled={processing} title={recording ? "Stop public voice recording" : "Record a publicly stored voice note"} type="button" onClick={() => recording ? stop() : void start()}>{processing ? "…" : recording ? <span className="recording-stop" /> : <ControlRoomIcon name="microphone" />}</button>;
}

function taxProfile(durationMillis: number): { sampleRate: number; frequency: number; level: string } {
  if (durationMillis <= 15_000) return { sampleRate: 48_000, frequency: 16_000, level: "mild low-pass" };
  if (durationMillis <= 45_000) return { sampleRate: 24_000, frequency: 8_000, level: "24khz" };
  return { sampleRate: 8_000, frequency: 3_400, level: "telephone" };
}

async function renderTaxedAudio(source: AudioBuffer, sampleRate: number, frequency: number): Promise<Blob> {
  const frames = Math.ceil(source.duration * sampleRate);
  const context = new OfflineAudioContext(1, frames, sampleRate);
  const node = context.createBufferSource();
  const buffer = context.createBuffer(1, source.length, source.sampleRate);
  const channel = buffer.getChannelData(0);
  for (let index = 0; index < source.length; index += 1) channel[index] = source.getChannelData(0)[index] ?? 0;
  node.buffer = buffer;
  const filter = context.createBiquadFilter();
  filter.type = "lowpass";
  filter.frequency.value = frequency;
  node.connect(filter).connect(context.destination);
  node.start();
  return wave(await context.startRendering());
}

function wave(buffer: AudioBuffer): Blob {
  const samples = buffer.getChannelData(0);
  const view = new DataView(new ArrayBuffer(44 + samples.length * 2));
  write(view, 0, "RIFF");
  view.setUint32(4, 36 + samples.length * 2, true);
  write(view, 8, "WAVEfmt ");
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, buffer.sampleRate, true);
  view.setUint32(28, buffer.sampleRate * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  write(view, 36, "data");
  view.setUint32(40, samples.length * 2, true);
  for (let index = 0; index < samples.length; index += 1) view.setInt16(44 + index * 2, Math.max(-1, Math.min(1, samples[index])) * 0x7fff, true);
  return new Blob([view], { type: "audio/wav" });
}

function write(view: DataView, offset: number, value: string): void {
  for (let index = 0; index < value.length; index += 1) view.setUint8(offset + index, value.charCodeAt(index));
}
