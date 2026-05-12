#!/usr/bin/env python3
import argparse
import base64
import datetime as _dt
import os
import re
import signal
import socket
import ssl
import struct
import subprocess
import sys
import tempfile
import threading
import time
import urllib.parse
from pathlib import Path

try:
    import socks as pysocks
except ImportError:
    pysocks = None


BUFFER_SIZE = 65536
HEADER_LIMIT = 1024 * 1024
BODY_PREVIEW_TEXT_LIMIT = 0
WINDOWS_CTRL_HANDLER = None


def now():
    return _dt.datetime.now().strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]


def safe_host(host):
    return re.sub(r"[^A-Za-z0-9_.-]", "_", host)


def text_or_base64(data):
    if not data:
        return "text", ""
    try:
        return "text", data.decode("utf-8")
    except UnicodeDecodeError:
        return "base64", base64.b64encode(data).decode("ascii")


class CaptureLogger:
    def __init__(self, log_path):
        self.path = Path(log_path)
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.lock = threading.Lock()
        self.file = self.path.open("a", encoding="utf-8", newline="\n")

    def close(self):
        with self.lock:
            self.file.close()

    def line(self, request_id, message):
        with self.lock:
            self.file.write(f"[{now()}] [{request_id}] {message}\n")
            self.file.flush()

    def block(self, request_id, title, data):
        kind, content = text_or_base64(data)
        with self.lock:
            self.file.write(f"[{now()}] [{request_id}] --- {title} bytes={len(data)} encoding={kind} ---\n")
            self.file.write(content)
            if content and not content.endswith("\n"):
                self.file.write("\n")
            self.file.write(f"[{now()}] [{request_id}] --- end {title} ---\n")
            self.file.flush()


class CertStore:
    def __init__(self, root_dir, openssl):
        self.root = Path(root_dir)
        self.openssl = openssl
        self.ca_key = self.root / "ca.key.pem"
        self.ca_cert = self.root / "ca.cert.pem"
        self.openssl_config = self.root / "openssl.cnf"
        self.cert_dir = self.root / "certs"
        self.cert_dir.mkdir(parents=True, exist_ok=True)
        self.lock = threading.Lock()
        self._ensure_ca()

    def _run(self, args, input_text=None):
        result = subprocess.run(
            [self.openssl, *args],
            input=input_text,
            text=True,
            capture_output=True,
            check=False,
        )
        if result.returncode != 0:
            raise RuntimeError(result.stderr.strip() or result.stdout.strip())

    def _ensure_ca(self):
        self.root.mkdir(parents=True, exist_ok=True)
        self._ensure_openssl_config()
        if self.ca_key.exists() and self.ca_cert.exists():
            return
        self._run(["genrsa", "-out", str(self.ca_key), "2048"])
        self._run(
            [
                "req",
                "-config",
                str(self.openssl_config),
                "-x509",
                "-new",
                "-nodes",
                "-key",
                str(self.ca_key),
                "-sha256",
                "-days",
                "3650",
                "-out",
                str(self.ca_cert),
                "-subj",
                "/CN=Codex Local Capture Proxy CA",
            ]
        )

    def _ensure_openssl_config(self):
        if self.openssl_config.exists():
            return
        self.openssl_config.write_text(
            "\n".join(
                [
                    "[req]",
                    "prompt = no",
                    "distinguished_name = req_distinguished_name",
                    "",
                    "[req_distinguished_name]",
                    "CN = localhost",
                    "",
                ]
            ),
            encoding="utf-8",
        )

    def cert_for(self, host):
        host = host.split(":")[0].strip().lower()
        if not host:
            raise ValueError("empty host")
        name = safe_host(host)
        key_path = self.cert_dir / f"{name}.key.pem"
        cert_path = self.cert_dir / f"{name}.cert.pem"
        csr_path = self.cert_dir / f"{name}.csr.pem"
        ext_path = self.cert_dir / f"{name}.ext"
        with self.lock:
            if key_path.exists() and cert_path.exists():
                return cert_path, key_path
            alt_name = "IP" if re.fullmatch(r"\d+\.\d+\.\d+\.\d+", host) else "DNS"
            ext_path.write_text(
                "\n".join(
                    [
                        "authorityKeyIdentifier=keyid,issuer",
                        "basicConstraints=CA:FALSE",
                        "keyUsage = digitalSignature, keyEncipherment",
                        "extendedKeyUsage = serverAuth",
                        f"subjectAltName = {alt_name}:{host}",
                        "",
                    ]
                ),
                encoding="utf-8",
            )
            self._run(["genrsa", "-out", str(key_path), "2048"])
            self._run(
                [
                    "req",
                    "-config",
                    str(self.openssl_config),
                    "-new",
                    "-key",
                    str(key_path),
                    "-out",
                    str(csr_path),
                    "-subj",
                    f"/CN={host}",
                ]
            )
            self._run(
                [
                    "x509",
                    "-req",
                    "-in",
                    str(csr_path),
                    "-CA",
                    str(self.ca_cert),
                    "-CAkey",
                    str(self.ca_key),
                    "-CAcreateserial",
                    "-out",
                    str(cert_path),
                    "-days",
                    "825",
                    "-sha256",
                    "-extfile",
                    str(ext_path),
                ]
            )
            return cert_path, key_path

    def has_cert_for(self, host):
        host = host.split(":")[0].strip().lower()
        if not host:
            return False
        name = safe_host(host)
        key_path = self.cert_dir / f"{name}.key.pem"
        cert_path = self.cert_dir / f"{name}.cert.pem"
        return key_path.exists() and cert_path.exists()


def recv_until(sock, delimiter=b"\r\n\r\n", limit=HEADER_LIMIT):
    data = bytearray()
    while delimiter not in data:
        chunk = sock.recv(4096)
        if not chunk:
            break
        data.extend(chunk)
        if len(data) > limit:
            raise ValueError("header too large")
    return bytes(data)


def split_headers(raw):
    header_end = raw.find(b"\r\n\r\n")
    if header_end < 0:
        return raw, b""
    return raw[:header_end], raw[header_end + 4 :]


def parse_header_lines(header_bytes):
    text = header_bytes.decode("iso-8859-1")
    lines = text.split("\r\n")
    start = lines[0] if lines else ""
    headers = []
    for line in lines[1:]:
        if not line or ":" not in line:
            continue
        key, value = line.split(":", 1)
        headers.append((key.strip(), value.strip()))
    return start, headers


def header_get(headers, key):
    key = key.lower()
    for k, v in headers:
        if k.lower() == key:
            return v
    return ""


def header_has_token(headers, key, token):
    token = token.lower()
    value = header_get(headers, key).lower()
    return any(part.strip() == token for part in value.split(","))


def read_body(sock, already, headers):
    body = bytearray(already)
    length = header_get(headers, "Content-Length")
    if length:
        remaining = max(0, int(length) - len(body))
        while remaining > 0:
            chunk = sock.recv(min(BUFFER_SIZE, remaining))
            if not chunk:
                break
            body.extend(chunk)
            remaining -= len(chunk)
    return bytes(body)


class BufferedSocketReader:
    def __init__(self, sock, initial=b"", stop_event=None):
        self.sock = sock
        self.buffer = bytearray(initial)
        self.stop_event = stop_event

    def read_exact(self, size):
        while len(self.buffer) < size:
            if self.stop_event and self.stop_event.is_set():
                break
            chunk = self.sock.recv(min(BUFFER_SIZE, max(1, size - len(self.buffer))))
            if not chunk:
                break
            self.buffer.extend(chunk)
        data = bytes(self.buffer[:size])
        del self.buffer[:size]
        return data

    def read_until(self, delimiter, limit=HEADER_LIMIT):
        while delimiter not in self.buffer:
            if self.stop_event and self.stop_event.is_set():
                break
            chunk = self.sock.recv(4096)
            if not chunk:
                break
            self.buffer.extend(chunk)
            if len(self.buffer) > limit:
                raise ValueError("buffered read too large")
        index = self.buffer.find(delimiter)
        if index < 0:
            data = bytes(self.buffer)
            self.buffer.clear()
            return data
        end = index + len(delimiter)
        data = bytes(self.buffer[:end])
        del self.buffer[:end]
        return data

    def read_available(self):
        data = bytes(self.buffer)
        self.buffer.clear()
        return data


def relay_response_body(upstream, client, headers, initial, stop_event):
    body = bytearray()

    def forward(data):
        if not data:
            return
        client.sendall(data)
        body.extend(data)

    length = header_get(headers, "Content-Length")
    if length:
        remaining = max(0, int(length))
        if initial:
            chunk = initial[:remaining]
            forward(chunk)
            remaining -= len(chunk)
        while remaining > 0 and not stop_event.is_set():
            chunk = upstream.recv(min(BUFFER_SIZE, remaining))
            if not chunk:
                break
            forward(chunk)
            remaining -= len(chunk)
        return bytes(body)

    reader = BufferedSocketReader(upstream, initial, stop_event)
    if header_has_token(headers, "Transfer-Encoding", "chunked"):
        while not stop_event.is_set():
            size_line = reader.read_until(b"\r\n")
            if not size_line:
                break
            forward(size_line)
            size_text = size_line.split(b";", 1)[0].strip()
            chunk_size = int(size_text, 16)
            chunk = reader.read_exact(chunk_size + 2)
            forward(chunk)
            if chunk_size == 0:
                trailers = reader.read_until(b"\r\n\r\n")
                forward(trailers)
                break
        return bytes(body)

    forward(reader.read_available())
    while not stop_event.is_set():
        try:
            chunk = upstream.recv(BUFFER_SIZE)
        except socket.timeout:
            break
        if not chunk:
            break
        forward(chunk)
    return bytes(body)


class UpstreamProxy:
    def __init__(self, proxy_url):
        if "://" not in proxy_url:
            proxy_url = f"http://{proxy_url}"
        parsed = urllib.parse.urlsplit(proxy_url)
        scheme = parsed.scheme.lower()
        if scheme not in ("http", "socks5"):
            raise ValueError("--proxy supports http:// or socks5:// proxies, e.g. http://127.0.0.1:10809 or socks5://user:pass@127.0.0.1:1080")
        if not parsed.hostname:
            raise ValueError(f"bad proxy URL: {proxy_url!r}")
        if scheme == "socks5" and pysocks is None:
            raise RuntimeError("socks5 upstream proxy requires PySocks: pip install pysocks")

        self.scheme = scheme
        self.url = proxy_url
        self.host = parsed.hostname
        self.port = parsed.port or (1080 if scheme == "socks5" else 80)
        self.username = urllib.parse.unquote(parsed.username) if parsed.username else None
        self.password = urllib.parse.unquote(parsed.password or "") if parsed.username else None
        self.auth_header = None
        if scheme == "http" and parsed.username is not None:
            token = base64.b64encode(f"{self.username}:{self.password}".encode("utf-8")).decode("ascii")
            self.auth_header = f"Basic {token}"

    def display(self):
        return f"{self.scheme}://{self.host}:{self.port}"

    def _socks5_socket(self, dest_host, dest_port):
        s = pysocks.socksocket(socket.AF_INET, socket.SOCK_STREAM)
        s.set_proxy(
            pysocks.SOCKS5,
            self.host,
            self.port,
            username=self.username,
            password=self.password,
        )
        s.settimeout(30)
        s.connect((dest_host, dest_port))
        return s

    def connect_forward(self):
        if self.scheme == "socks5":
            raise RuntimeError("socks5 upstream does not support HTTP absolute-form forwarding; use connect_tunnel instead")
        return socket.create_connection((self.host, self.port), timeout=30)

    def connect_tunnel(self, host, port):
        if self.scheme == "socks5":
            return self._socks5_socket(host, port)
        raw = self.connect_forward()
        target = format_host_port(host, port)
        headers = [
            f"CONNECT {target} HTTP/1.1",
            f"Host: {target}",
            "Proxy-Connection: keep-alive",
        ]
        if self.auth_header:
            headers.append(f"Proxy-Authorization: {self.auth_header}")
        request = "\r\n".join(headers) + "\r\n\r\n"
        raw.sendall(request.encode("iso-8859-1"))
        response = recv_until(raw)
        header_bytes, _ = split_headers(response)
        start, _ = parse_header_lines(header_bytes)
        parts = start.split(" ", 2)
        if len(parts) < 2 or not parts[1].isdigit() or int(parts[1]) // 100 != 2:
            raw.close()
            raise RuntimeError(f"upstream proxy CONNECT failed: {start!r}")
        return raw


def format_host_port(host, port):
    if ":" in host and not host.startswith("["):
        return f"[{host}]:{port}"
    return f"{host}:{port}"


def rewrite_request_for_origin(
    start_line,
    headers,
    default_host,
    default_port,
    tls,
    absolute_form=False,
    proxy_authorization=None,
):
    parts = start_line.split(" ")
    if len(parts) < 3:
        raise ValueError(f"bad request line: {start_line!r}")
    method, target, version = parts[0], parts[1], parts[2]
    scheme = "https" if tls else "http"
    host = default_host
    port = default_port
    path = target

    parsed = urllib.parse.urlsplit(target)
    if parsed.scheme and parsed.netloc:
        scheme = parsed.scheme
        host = parsed.hostname or host
        port = parsed.port or (443 if scheme == "https" else 80)
        path = urllib.parse.urlunsplit(("", "", parsed.path or "/", parsed.query, ""))
    elif not path.startswith("/"):
        path = "/" + path

    out_headers = []
    saw_host = False
    for key, value in headers:
        lk = key.lower()
        if lk in {"proxy-connection", "proxy-authorization"}:
            continue
        if lk == "connection":
            continue
        if lk == "host":
            saw_host = True
            value = host if port in (80, 443) else f"{host}:{port}"
        out_headers.append((key, value))
    if not saw_host:
        out_headers.insert(0, ("Host", host if port in (80, 443) else f"{host}:{port}"))
    out_headers.append(("Connection", "close"))

    request_target = path or "/"
    if absolute_form and scheme == "http":
        netloc = host if port in (80, 443) else format_host_port(host, port)
        request_target = f"{scheme}://{netloc}{request_target}"

    if proxy_authorization:
        out_headers.append(("Proxy-Authorization", proxy_authorization))

    rebuilt = f"{method} {request_target} {version}\r\n"
    rebuilt += "".join(f"{k}: {v}\r\n" for k, v in out_headers)
    rebuilt += "\r\n"
    return scheme, host, port, path or "/", rebuilt.encode("iso-8859-1")


def connect_tcp(host, port, upstream_proxy=None):
    if upstream_proxy:
        return upstream_proxy.connect_tunnel(host, port)
    return socket.create_connection((host, port), timeout=30)


def connect_upstream(host, port, tls, server_name, upstream_proxy=None, proxy_forward=False):
    if upstream_proxy and proxy_forward:
        raw = upstream_proxy.connect_forward()
    else:
        raw = connect_tcp(host, port, upstream_proxy)
    if tls:
        ctx = ssl.create_default_context()
        return ctx.wrap_socket(raw, server_hostname=server_name)
    return raw


class ProxyServer:
    def __init__(self, host, port, logger, cert_store=None, upstream_proxy=None, mitm_targets=None, auth=None):
        self.host = host
        self.port = port
        self.logger = logger
        self.cert_store = cert_store
        self.upstream_proxy = upstream_proxy
        self.mitm_targets = [t.lower() for t in (mitm_targets or [])]
        self.auth = auth
        self.counter = 0
        self.counter_lock = threading.Lock()
        self.stop_event = threading.Event()
        self.server_socket = None
        self.sockets = set()
        self.sockets_lock = threading.Lock()
        self.threads = set()
        self.threads_lock = threading.Lock()

    def next_id(self):
        with self.counter_lock:
            self.counter += 1
            return f"req-{self.counter:06d}"

    def track_socket(self, sock):
        with self.sockets_lock:
            self.sockets.add(sock)

    def untrack_socket(self, sock):
        with self.sockets_lock:
            self.sockets.discard(sock)

    def track_thread(self, thread):
        with self.threads_lock:
            self.threads.add(thread)

    def untrack_current_thread(self):
        current = threading.current_thread()
        with self.threads_lock:
            self.threads.discard(current)

    def join_threads(self, timeout=5):
        deadline = time.time() + timeout
        while True:
            with self.threads_lock:
                threads = [thread for thread in self.threads if thread is not threading.current_thread()]
            if not threads:
                return
            remaining = deadline - time.time()
            if remaining <= 0:
                return
            for thread in threads:
                thread.join(min(0.2, remaining))

    def close_socket(self, sock):
        try:
            sock.shutdown(socket.SHUT_RDWR)
        except Exception:
            pass
        try:
            sock.close()
        except Exception:
            pass

    def shutdown(self):
        if self.stop_event.is_set():
            return
        self.stop_event.set()
        if self.server_socket:
            self.close_socket(self.server_socket)
        with self.sockets_lock:
            sockets = list(self.sockets)
        for sock in sockets:
            self.close_socket(sock)

    def should_mitm(self, host):
        if not self.cert_store or not self.mitm_targets:
            return False
        host_clean = host.lower().strip(".")
        for pattern in self.mitm_targets:
            if host_clean == pattern or host_clean.endswith(f".{pattern}"):
                return True
        return False

    def _check_proxy_auth(self, headers):
        expected = base64.b64encode(self.auth.encode("utf-8")).decode("ascii")
        auth_value = header_get(headers, "Proxy-Authorization")
        if not auth_value:
            return False
        parts = auth_value.split(None, 1)
        if len(parts) != 2 or parts[0].lower() != "basic":
            return False
        return parts[1] == expected

    def serve_forever(self):
        server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        server.bind((self.host, self.port))
        server.listen(200)
        server.settimeout(0.5)
        self.server_socket = server
        print(f"Proxy listening on {self.host}:{self.port}")
        try:
            while not self.stop_event.is_set():
                try:
                    client, addr = server.accept()
                except socket.timeout:
                    continue
                except OSError:
                    if self.stop_event.is_set():
                        break
                    raise
                self.track_socket(client)
                thread = threading.Thread(target=self.handle_client, args=(client, addr))
                self.track_thread(thread)
                thread.start()
        finally:
            self.shutdown()
            self.join_threads()

    def handle_client(self, client, addr):
        request_id = self.next_id()
        try:
            client.settimeout(120)
            raw = recv_until(client)
            if not raw:
                return
            header_bytes, already = split_headers(raw)
            start, headers = parse_header_lines(header_bytes)
            self.logger.line(request_id, f"client={addr[0]}:{addr[1]} start={start!r}")
            if self.auth and not self._check_proxy_auth(headers):
                self.logger.line(request_id, "AUTH FAILED - returning 407")
                client.sendall(
                    b"HTTP/1.1 407 Proxy Authentication Required\r\n"
                    b"Proxy-Authenticate: Basic realm=\"proxy\"\r\n"
                    b"Content-Length: 0\r\n"
                    b"\r\n"
                )
                return
            if start.upper().startswith("CONNECT "):
                self.handle_connect(client, request_id, start)
            else:
                body = read_body(client, already, headers)
                self.handle_one_request(client, request_id, start, headers, body, None, None, False)
        except Exception as exc:
            self.logger.line(request_id, f"ERROR {type(exc).__name__}: {exc}")
        finally:
            self.untrack_socket(client)
            try:
                client.close()
            except Exception:
                pass
            self.untrack_current_thread()

    def handle_connect(self, client, request_id, start):
        target = start.split(" ")[1]
        host, _, port_text = target.partition(":")
        port = int(port_text or "443")
        self.logger.line(request_id, f"CONNECT target={host}:{port}")
        client.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
        self._relay_after_connect(client, request_id, host, port)

    def _relay_after_connect(self, client, request_id, host, port):
        if not self.should_mitm(host):
            self.tunnel_blind(client, request_id, host, port)
            return

        cert, key = self.cert_store.cert_for(host)
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ctx.load_cert_chain(certfile=str(cert), keyfile=str(key))
        tls_client = ctx.wrap_socket(client, server_side=True)
        self.track_socket(tls_client)
        try:
            while not self.stop_event.is_set():
                raw = recv_until(tls_client)
                if not raw:
                    break
                header_bytes, already = split_headers(raw)
                start, headers = parse_header_lines(header_bytes)
                if not start:
                    break
                body = read_body(tls_client, already, headers)
                child_id = self.next_id()
                self.handle_one_request(tls_client, child_id, start, headers, body, host, port, True)
                if header_get(headers, "Connection").lower() == "close":
                    break
        finally:
            self.untrack_socket(tls_client)
            self.close_socket(tls_client)

    def tunnel_blind(self, client, request_id, host, port):
        upstream = connect_tcp(host, port, self.upstream_proxy)
        self.track_socket(upstream)
        if self.upstream_proxy:
            self.logger.line(request_id, f"blind CONNECT tunnel via upstream proxy {self.upstream_proxy.display()}")
        else:
            self.logger.line(request_id, "blind CONNECT tunnel; enable --mitm and trust CA to see HTTPS bodies")

        def pump(src, dst, label):
            try:
                while not self.stop_event.is_set():
                    data = src.recv(BUFFER_SIZE)
                    if not data:
                        break
                    dst.sendall(data)
            except Exception:
                pass
            finally:
                self.logger.line(request_id, f"blind tunnel closed side={label}")
                try:
                    dst.shutdown(socket.SHUT_RDWR)
                except Exception:
                    pass

        t1 = threading.Thread(target=pump, args=(client, upstream, "client->upstream"))
        t2 = threading.Thread(target=pump, args=(upstream, client, "upstream->client"))
        t1.start()
        t2.start()
        t1.join()
        t2.join()
        self.untrack_socket(upstream)
        self.close_socket(upstream)

    def handle_one_request(self, client, request_id, start, headers, body, default_host, default_port, tls):
        can_absolute = (
            self.upstream_proxy is not None
            and self.upstream_proxy.scheme == "http"
            and not tls
        )
        scheme, host, port, path, outbound_head = rewrite_request_for_origin(
            start,
            headers,
            default_host,
            default_port,
            tls,
            absolute_form=can_absolute,
            proxy_authorization=self.upstream_proxy.auth_header if can_absolute else None,
        )
        upstream_tls = scheme == "https"
        via = f" via {self.upstream_proxy.display()}" if self.upstream_proxy else ""
        self.logger.line(request_id, f"OUTBOUND{via} {start!r} -> {scheme}://{host}:{port}{path}")
        self.logger.block(request_id, "request headers", outbound_head)
        self.logger.block(request_id, "request body", body)

        proxy_forward = can_absolute and not upstream_tls
        use_connect = self.upstream_proxy if not proxy_forward else None
        upstream = connect_upstream(
            host,
            port,
            upstream_tls,
            host,
            use_connect,
            proxy_forward=proxy_forward,
        )
        self.track_socket(upstream)
        try:
            upstream.sendall(outbound_head)
            if body:
                upstream.sendall(body)

            resp_header_raw = recv_until(upstream)
            if not resp_header_raw:
                return
            resp_header_bytes, resp_already = split_headers(resp_header_raw)
            resp_start, resp_headers = parse_header_lines(resp_header_bytes)
            self.logger.line(request_id, f"INBOUND {resp_start!r}")
            self.logger.block(request_id, "response headers", resp_header_bytes + b"\r\n\r\n")
            client.sendall(resp_header_bytes + b"\r\n\r\n")

            response_body = relay_response_body(upstream, client, resp_headers, resp_already, self.stop_event)
            self.logger.block(request_id, "response body", response_body)
        finally:
            self.untrack_socket(upstream)
            try:
                self.close_socket(upstream)
            except Exception:
                pass


class Socks5Server:
    def __init__(self, host, port, proxy, auth=None):
        self.host = host
        self.port = port
        self.proxy = proxy
        self.auth = auth
        self.server_socket = None

    def shutdown(self):
        if self.server_socket:
            self.proxy.close_socket(self.server_socket)

    def serve_forever(self):
        server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        server.bind((self.host, self.port))
        server.listen(200)
        server.settimeout(0.5)
        self.server_socket = server
        print(f"SOCKS5 proxy listening on {self.host}:{self.port}")
        try:
            while not self.proxy.stop_event.is_set():
                try:
                    client, addr = server.accept()
                except socket.timeout:
                    continue
                except OSError:
                    if self.proxy.stop_event.is_set():
                        break
                    raise
                self.proxy.track_socket(client)
                thread = threading.Thread(target=self.handle_client, args=(client, addr))
                self.proxy.track_thread(thread)
                thread.start()
        finally:
            self.shutdown()

    @staticmethod
    def _recv_exact(sock, n):
        data = bytearray()
        while len(data) < n:
            chunk = sock.recv(n - len(data))
            if not chunk:
                return None
            data.extend(chunk)
        return bytes(data)

    def _socks5_reply(self, client, rep):
        client.sendall(struct.pack("!BBBB4sH", 0x05, rep, 0x00, 0x01, b"\x00\x00\x00\x00", 0))

    def handle_client(self, client, addr):
        request_id = self.proxy.next_id()
        try:
            client.settimeout(120)

            ver_nmethods = self._recv_exact(client, 2)
            if not ver_nmethods or ver_nmethods[0] != 0x05:
                return
            methods = self._recv_exact(client, ver_nmethods[1])
            if not methods:
                return

            if self.auth:
                if 0x02 not in methods:
                    client.sendall(b"\x05\xFF")
                    return
                client.sendall(b"\x05\x02")

                sub_ver = self._recv_exact(client, 1)
                if not sub_ver or sub_ver[0] != 0x01:
                    return
                ulen = self._recv_exact(client, 1)
                if not ulen:
                    return
                username = self._recv_exact(client, ulen[0])
                if not username:
                    return
                plen = self._recv_exact(client, 1)
                if not plen:
                    return
                password = self._recv_exact(client, plen[0])
                if not password:
                    return

                expected_user, _, expected_pass = self.auth.partition(":")
                if username.decode("utf-8", errors="replace") != expected_user or \
                   password.decode("utf-8", errors="replace") != expected_pass:
                    self.proxy.logger.line(request_id, f"SOCKS5 AUTH FAILED from {addr[0]}:{addr[1]}")
                    client.sendall(b"\x01\x01")
                    return
                client.sendall(b"\x01\x00")
            else:
                if 0x00 not in methods:
                    client.sendall(b"\x05\xFF")
                    return
                client.sendall(b"\x05\x00")

            header = self._recv_exact(client, 4)
            if not header:
                return
            ver, cmd, _, atyp = header
            if ver != 0x05:
                return
            if cmd != 0x01:
                self._socks5_reply(client, 0x07)
                return

            if atyp == 0x01:
                raw_addr = self._recv_exact(client, 4)
                if not raw_addr:
                    return
                dest_host = socket.inet_ntoa(raw_addr)
            elif atyp == 0x03:
                name_len_byte = self._recv_exact(client, 1)
                if not name_len_byte:
                    return
                domain = self._recv_exact(client, name_len_byte[0])
                if not domain:
                    return
                dest_host = domain.decode("ascii")
            elif atyp == 0x04:
                raw_addr = self._recv_exact(client, 16)
                if not raw_addr:
                    return
                dest_host = socket.inet_ntop(socket.AF_INET6, raw_addr)
            else:
                self._socks5_reply(client, 0x08)
                return

            raw_port = self._recv_exact(client, 2)
            if not raw_port:
                return
            dest_port = struct.unpack("!H", raw_port)[0]

            self.proxy.logger.line(request_id, f"SOCKS5 CONNECT {dest_host}:{dest_port} from {addr[0]}:{addr[1]}")
            self._socks5_reply(client, 0x00)
            self.proxy._relay_after_connect(client, request_id, dest_host, dest_port)

        except Exception as exc:
            self.proxy.logger.line(request_id, f"SOCKS5 ERROR {type(exc).__name__}: {exc}")
        finally:
            self.proxy.untrack_socket(client)
            try:
                client.close()
            except Exception:
                pass
            self.proxy.untrack_current_thread()


def install_shutdown_handlers(*servers):
    def stop(signum=None, frame=None):
        print("\nStopping proxy.")
        for s in servers:
            s.shutdown()

    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)
    if hasattr(signal, "SIGBREAK"):
        signal.signal(signal.SIGBREAK, stop)

    if os.name == "nt":
        try:
            import ctypes

            handler_type = ctypes.WINFUNCTYPE(ctypes.c_bool, ctypes.c_ulong)
            close_events = {2, 5, 6}

            def console_handler(ctrl_type):
                stop()
                if ctrl_type in close_events:
                    def force_exit():
                        for s in servers:
                            if hasattr(s, "join_threads"):
                                s.join_threads(timeout=2)
                        os._exit(0)

                    threading.Thread(target=force_exit, daemon=True).start()
                return True

            global WINDOWS_CTRL_HANDLER
            WINDOWS_CTRL_HANDLER = handler_type(console_handler)
            ctypes.windll.kernel32.SetConsoleCtrlHandler(WINDOWS_CTRL_HANDLER, True)
        except Exception:
            pass


def split_csv_args(values):
    result = []
    for value in values or []:
        result.extend(part.strip() for part in value.split(",") if part.strip())
    return result


def main():
    default_log = Path("logs") / f"proxy-{_dt.datetime.now().strftime('%Y%m%d-%H%M%S')}.log"
    parser = argparse.ArgumentParser(description="Small HTTP/HTTPS capture proxy for local debugging.")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8888)
    parser.add_argument("--socks-port", type=int, default=None, help="SOCKS5 proxy listen port (e.g. 1080). If not set, SOCKS5 entry is disabled.")
    parser.add_argument("--auth", default=None, help="Inbound proxy auth credentials, format user:pass. Applies to both HTTP and SOCKS5 entry.")
    parser.add_argument("--log", default=str(default_log))
    parser.add_argument("--mitm", action="store_true", help="Intercept HTTPS CONNECT traffic with a local CA.")
    parser.add_argument("--cert-dir", default=".proxy-certs")
    parser.add_argument("--openssl", default=os.environ.get("OPENSSL", "openssl"))
    parser.add_argument("--proxy", help="Upstream proxy for outbound traffic, e.g. http://127.0.0.1:10809 or socks5://user:pass@127.0.0.1:1080.")
    parser.add_argument(
        "--mitm-target",
        action="append",
        default=[],
        help="Domain suffixes to MITM (e.g. api.openai.com). Only these domains will be intercepted; all others are tunneled blindly.",
    )
    args = parser.parse_args()

    upstream_proxy = UpstreamProxy(args.proxy) if args.proxy else None
    if upstream_proxy:
        print(f"Outbound traffic will use upstream proxy: {upstream_proxy.display()}")

    cert_store = None
    if args.mitm:
        cert_store = CertStore(args.cert_dir, args.openssl)
        print(f"MITM CA certificate: {cert_store.ca_cert.resolve()}")
        print("Trust this CA, or set NODE_EXTRA_CA_CERTS to this file for Node/VS Code extension traffic.")

    logger = CaptureLogger(args.log)
    print(f"Writing capture log to: {Path(args.log).resolve()}")
    mitm_targets = split_csv_args(args.mitm_target)
    if mitm_targets:
        print(f"MITM target domains: {', '.join(mitm_targets)}")
    else:
        print("MITM disabled (no --mitm-target specified, all traffic tunneled blindly)")

    if args.auth:
        print(f"Inbound proxy authentication enabled (user: {args.auth.split(':')[0]})")

    server = ProxyServer(args.host, args.port, logger, cert_store, upstream_proxy, mitm_targets, auth=args.auth)

    socks_server = None
    if args.socks_port:
        socks_server = Socks5Server(args.host, args.socks_port, server, auth=args.auth)

    all_servers = [s for s in [server, socks_server] if s]
    install_shutdown_handlers(*all_servers)

    if socks_server:
        threading.Thread(target=socks_server.serve_forever, daemon=True).start()

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        for s in all_servers:
            s.shutdown()
        server.join_threads()
        logger.close()


if __name__ == "__main__":
    main()
