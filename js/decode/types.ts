import { Frame } from "../types"

export type DecodeFrame = {
    frame: Frame,
    decodeFrame: Uint8Array,
    prevDecodeFrame: Uint8Array | null,
    length: number,
}
