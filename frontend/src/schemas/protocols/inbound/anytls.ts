import { z } from 'zod';

// An AnyTLS inbound client. Each client is one password the anytls-server
// sidecar serves; the password is the identity the node bills traffic to.
export const AnytlsClientSchema = z.object({
  password: z.string().default(''),
  email: z.string().min(1),
  limitIp: z.number().int().min(0).default(0),
  totalGB: z.number().int().min(0).default(0),
  expiryTime: z.number().int().default(0),
  enable: z.boolean().default(true),
  tgId: z
    .union([z.number(), z.string()])
    .transform((v) => Number(v) || 0)
    .default(0),
  subId: z.string().default(''),
  comment: z.string().default(''),
  reset: z.number().int().min(0).default(0),
  created_at: z.number().int().optional(),
  updated_at: z.number().int().optional(),
});
export type AnytlsClient = z.infer<typeof AnytlsClientSchema>;

// AnyTLS inbound. Served by an anytls-server sidecar, not Xray, so it has no
// stream settings — the protocol carries its own TLS.
export const AnytlsInboundSettingsSchema = z.object({
  clients: z.array(AnytlsClientSchema).default([]),
  // Certificate paths on the panel host. Without both, the node falls back to
  // an ephemeral self-signed cert that only insecure clients accept.
  sni: z.string().default(''),
  certFile: z.string().default(''),
  keyFile: z.string().default(''),
  // Where a connection that fails auth is relayed, which is what makes the
  // port answer an active prober like an ordinary web server.
  forward: z.string().default(''),
  paddingScheme: z.string().optional(),
  debug: z.boolean().optional(),
  // When set, the node dials out through a loopback SOCKS bridge in the Xray
  // config so egress obeys routing rules. The bridge port is backend-owned.
  routeThroughXray: z.boolean().optional(),
  outboundTag: z.string().optional(),
  routeXrayPort: z.number().int().min(0).max(65535).optional(),
});
export type AnytlsInboundSettings = z.infer<typeof AnytlsInboundSettingsSchema>;
