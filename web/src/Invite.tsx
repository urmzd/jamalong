import { Check, Copy, RefreshCw } from "lucide-react";
import { useMemo, useState } from "react";
import { encode } from "uqr";

function QrCode({ text }: { text: string }) {
  const { path, size } = useMemo(() => {
    const qr = encode(text, { border: 2 });
    let d = "";
    qr.data.forEach((row, y) => {
      row.forEach((on, x) => {
        if (on) d += `M${x} ${y}h1v1h-1z`;
      });
    });
    return { path: d, size: qr.size };
  }, [text]);
  return (
    <svg className="qr" viewBox={`0 0 ${size} ${size}`} role="img" aria-label="QR code to join">
      <rect width={size} height={size} fill="#fff" />
      <path d={path} fill="#000" />
    </svg>
  );
}

export function Invite(props: { roomId: string; secret: string; onRotate(): void }) {
  const [copied, setCopied] = useState(false);
  const link = `${location.origin}/r/${props.roomId}#${props.secret}`;

  async function copy() {
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
      setTimeout(() => setCopied(false), 2_000);
    } catch {
      // Clipboard access can be refused. The QR code still works.
    }
  }

  return (
    <div className="invite">
      <QrCode text={link} />
      <div className="invite-actions">
        <h2>Scan to join</h2>
        <button className="btn" type="button" onClick={copy}>
          {copied ? <Check aria-hidden size={18} /> : <Copy aria-hidden size={18} />}
          {copied ? "Copied" : "Copy link"}
        </button>
        <button className="btn" type="button" onClick={props.onRotate}>
          <RefreshCw aria-hidden size={18} />
          New link
        </button>
        <p className="muted small">A new link turns the old one off.</p>
      </div>
    </div>
  );
}
