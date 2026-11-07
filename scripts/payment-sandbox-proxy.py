#!/usr/bin/env python3
"""Temporary storefront-only relay for an explicitly enabled payment sandbox.

Place a HTTPS tunnel in front of this loopback listener, never the admin server.
Does not handle payment status, signatures, or credentials; GoCMS handles those.
"""
import argparse
import http.client
import http.server
import pathlib
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parent.parent
FS_ROOTS = [ROOT / 'frontend/src/components/ui', ROOT / 'frontend/node_modules', ROOT / 'storefront/src', ROOT / 'storefront/node_modules']
EXTENSIONS = {'.js', '.jsx', '.ts', '.tsx', '.mjs', '.cjs', '.css', '.json', '.svg', '.map', '.woff', '.woff2', '.ttf'}
HOP_HEADERS = {'connection', 'keep-alive', 'proxy-authenticate', 'proxy-authorization', 'te', 'trailer', 'transfer-encoding', 'upgrade'}


def allowed(path, method):
    decoded = urllib.parse.unquote(urllib.parse.urlsplit(path).path)
    if '\\' in decoded or '%' in decoded or any(part in {'.', '..'} for part in decoded.split('/')):
        return False
    if decoded in {'/api/v1/auth/login', '/api/v1/auth/register', '/api/v1/auth/logout', '/api/v1/me'}:
        return method in {'GET', 'POST'}
    if decoded.startswith('/api/payments/webhooks/acct_'):
        return method == 'POST'
    if decoded == '/api/commerce/orders' or decoded.startswith('/api/commerce/orders/'):
        return method in {'GET', 'POST'}
    if decoded in {'/api/commerce/storefront', '/api/commerce/pricing', '/api/commerce/channels', '/api/commerce/variants', '/api/geography/countries', '/api/geography/regions', '/api/geography/cities'}:
        return method in {'GET', 'HEAD'}
    if method not in {'GET', 'HEAD'}:
        return False
    if decoded.startswith('/shop/@fs/'):
        target = pathlib.Path(decoded.removeprefix('/shop/@fs')).resolve()
        return target.suffix in EXTENSIONS and any(target.is_relative_to(root.resolve()) for root in FS_ROOTS)
    if decoded in {'/shop', '/shop/'} or decoded.startswith('/shop/'):
        return not any(part.startswith('.') for part in decoded.split('/') if part)
    return decoded.startswith(('/assets/', '/images/', '/css/', '/js/', '/skin/'))


class Relay(http.server.BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.0'

    def log_message(self, format, *args):
        # Do not log cookies, query strings, customer data, or payloads.
        return

    def relay(self):
        if not allowed(self.path, self.command):
            self.send_error(404)
            return
        if self.headers.get('Transfer-Encoding'):
            self.send_error(400)
            return
        try:
            size = int(self.headers.get('Content-Length', '0'))
        except ValueError:
            self.send_error(400)
            return
        if size < 0 or size > 1024 * 1024:
            self.send_error(413)
            return
        data = self.rfile.read(size) if size else None
        headers = {key: value for key, value in self.headers.items() if key.lower() not in HOP_HEADERS and key.lower() not in {'host', 'x-site-id', 'x-forwarded-host', 'x-forwarded-proto', 'x-scheme', 'authorization', 'cookie'}}
        headers['Host'] = f'127.0.0.1:{self.server.backend_port}'
        headers['X-Forwarded-Host'] = self.headers.get('Host', '')
        headers['X-Forwarded-Proto'] = 'https'
        headers['X-Site-Id'] = str(self.server.site_id)
        # Admin cookies have no reason to leave the browser through this relay.
        cookies = [part.strip() for part in self.headers.get('Cookie', '').split(';') if part.strip().startswith('gocms_user=')]
        headers.pop('Cookie', None)
        headers.pop('cookie', None)
        if cookies:
            headers['Cookie'] = '; '.join(cookies)
        connection = http.client.HTTPConnection('127.0.0.1', self.server.backend_port, timeout=40)
        try:
            connection.request(self.command, self.path, data, headers)
            response = connection.getresponse()
            payload = response.read(32 * 1024 * 1024 + 1)
            if len(payload) > 32 * 1024 * 1024:
                self.send_error(502)
                return
            self.send_response(response.status)
            for key, value in response.getheaders():
                if key.lower() not in HOP_HEADERS and key.lower() != 'content-length':
                    self.send_header(key, value)
            self.send_header('Content-Length', str(len(payload)))
            self.end_headers()
            if self.command != 'HEAD':
                self.wfile.write(payload)
        except (OSError, http.client.HTTPException):
            self.send_error(502)
        finally:
            connection.close()

    do_GET = do_HEAD = do_POST = do_PUT = do_PATCH = do_DELETE = do_OPTIONS = relay


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--site', type=int, required=True)
    parser.add_argument('--port', type=int, default=18081)
    parser.add_argument('--backend-port', type=int, default=18080)
    args = parser.parse_args()
    if args.site < 1:
        parser.error('--site must be a positive site ID')
    server = http.server.ThreadingHTTPServer(('127.0.0.1', args.port), Relay)
    server.daemon_threads = True
    server.site_id = args.site
    server.backend_port = args.backend_port
    print(f'Storefront-only relay listening on 127.0.0.1:{args.port}, site {args.site}', flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == '__main__':
    main()
