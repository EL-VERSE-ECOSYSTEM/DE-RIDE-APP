declare module 'qrcode' {
  export type QRCodeOptions = {
    errorCorrectionLevel?: 'L' | 'M' | 'Q' | 'H';
    margin?: number;
    width?: number;
    color?: { dark?: string; light?: string };
  };
  export function toDataURL(text: string, options?: QRCodeOptions): Promise<string>;
}
