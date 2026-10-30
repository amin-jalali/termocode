// Runs first (it is main.tsx's first import, and imports run in order):
// the safety hook goes in before anything renders, and the launch secret
// leaves the URL before any other module reads location.
import { installSafety } from "./lib/safety";
import { takePair, takeSecret } from "./net/auth";

installSafety();

/** secret is the launch secret from the URL, already removed from it. */
export const secret = takeSecret();

/** pairSecret is a pairing link's secret (#p=, from the laptop's QR code
 * or a share link), already removed from the URL. */
export const pairSecret = secret ? null : takePair();
