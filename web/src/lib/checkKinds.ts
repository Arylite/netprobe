export type CheckKind = "tcp" | "http" | "dns" | "tls" | "icmp" | "ntp" | "banner" | "closed" | "download" | "traceroute" | "domain";

interface KindInfo {
  value: CheckKind;
  label: string;
  target: string;
  // Absent when the kind takes no expectation.
  expect?: string;
  // The shortest interval the central accepts for it.
  minInterval: number;
}

export const KINDS: KindInfo[] = [
  { value: "tcp", label: "TCP connect", target: "host:port, for example example.com:443", minInterval: 1 },
  {
    value: "http",
    label: "HTTP request",
    target: "A URL, for example https://example.com/health",
    expect: "Optional: a status (200 or 2xx), contains:TEXT, absent:TEXT, separated by ;",
    minInterval: 1,
  },
  {
    value: "dns",
    label: "DNS lookup",
    target: "[TYPE ]name[@server], for example MX example.com@1.1.1.1",
    expect: "Optional: text that one of the answers must hold",
    minInterval: 1,
  },
  {
    value: "tls",
    label: "TLS certificate",
    target: "host[:port], for example example.com",
    expect: "Optional: days the certificate must still be valid (14)",
    minInterval: 60,
  },
  {
    value: "icmp",
    label: "ICMP ping",
    target: "A host or an address, for example 1.1.1.1",
    expect: "Optional: share of the 4 echoes that may be lost, in percent (50)",
    minInterval: 5,
  },
  {
    value: "ntp",
    label: "NTP clock",
    target: "host[:port], for example pool.ntp.org",
    expect: "Optional: offset of the clock that is tolerated (1s)",
    minInterval: 5,
  },
  {
    value: "banner",
    label: "Service banner",
    target: "host:port, for example example.com:22",
    expect: "Optional: text the first line must hold, for example SSH-2.0",
    minInterval: 1,
  },
  { value: "closed", label: "Port must be closed", target: "host:port that must not be reachable, for example db.example.com:5432", minInterval: 1 },
  {
    value: "download",
    label: "Download speed",
    target: "A URL of a file, for example https://example.com/100MB.bin",
    expect: "Optional: least speed in megabits per second",
    minInterval: 60,
  },
  {
    value: "traceroute",
    label: "Traceroute",
    target: "A host or an address, for example example.com",
    expect: "Optional: the most hops the path may have (30)",
    minInterval: 60,
  },
  {
    value: "domain",
    label: "Domain expiry",
    target: "A domain name, for example example.com",
    expect: "Optional: days that must remain before it expires (30)",
    minInterval: 3600,
  },
];

export function kindInfo(kind: string): KindInfo | undefined {
  return KINDS.find((k) => k.value === kind);
}
