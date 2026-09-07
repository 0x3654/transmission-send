#!/usr/bin/env python3
"""
tracker-top — крохотный сервер топа раздач NNMClub для плагина Lampa «Топ трекеров».

    GET /top?cat=video|all&pages=1..3[&limit=N]
        cat=video — только кино/сериалы/мультфильмы/аниме/документалка (поддерево
        разделов, список категорий парсится со страницы трекера автоматически);
        pages — сколько страниц топа взять (по 50 раздач, сортировка по сидам);
        limit — обрезать результат.

    GET /healthz

Ответ: {"source": "nnmclub", "fetched_at": ..., "items": [
    {"id","ru","orig","year","season","title","category","forum_id",
     "seeders","leechers","size","size_text","added","url","download"}]}

Кэш в памяти TTL секунд (TTL, по умолчанию 600). CORS: *. Зависимостей нет.

Переносимость: один файл, mirror задаётся NNM_BASE. Разворачивается где угодно —
micro, VPN-сервер — адрес меняется только в настройках плагина.
"""

import html as html_lib
import json
import os
import re
import sys
import time
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.request import Request, urlopen

PORT = int(os.environ.get('PORT', '8355'))
NNM_BASE = os.environ.get('NNM_BASE', 'https://nnmclub.to').rstrip('/')
TTL = int(os.environ.get('TTL', '600'))
UA = os.environ.get('UA', 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) '
                          'AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36')

PAGE_SIZE = 50

# корневые разделы «видео» на NNMClub (идентификаторы форумов)
VIDEO_ROOTS = {216, 318, 220, 224, 1311, 256, 264,   # кино (+театр, остальное)
               1219, 768, 769, 713,                    # сериалы
               576,                                    # документалистика
               724,                                    # детское видео/мультфильмы
               620, 624, 628}                          # аниме

_cache = {}


# ---------------- страница трекера

def fetch_page(path):
    url = NNM_BASE + path
    req = Request(url, headers={'User-Agent': UA, 'Accept-Encoding': 'identity'})
    with urlopen(req, timeout=20) as r:
        raw = r.read()
    try:
        return raw.decode('cp1251')
    except UnicodeDecodeError:
        return raw.decode('utf-8', errors='replace')


ROW_RE = re.compile(
    r'<tr[^>]*>(?:(?!</tr>).)*?href="viewtopic\.php\?t=(\d+)"[^>]*><b>(.*?)</b>(?:(?!</tr>).)*?</tr>',
    re.S)
CELL_RE = re.compile(r'<td[^>]*>(.*?)</td>', re.S)
TAG_RE = re.compile(r'<[^>]+>')
DL_RE = re.compile(r'href="download\.php\?id=(\d+)"')
FORUM_RE = re.compile(r'href="tracker\.php\?f=(\d+)"[^>]*>([^<]+)<')


def parse_rows(page_html):
    """строки таблицы раздач → список словарей"""
    items = []
    for m in ROW_RE.finditer(page_html):
        cells = CELL_RE.findall(m.group(0))
        if len(cells) < 9:
            continue
        title = html_lib.unescape(TAG_RE.sub('', cells[2])).strip()

        fm = FORUM_RE.search(cells[1])
        forum_id = int(fm.group(1)) if fm else 0
        category = html_lib.unescape(fm.group(2)).strip() if fm else ''

        dm = DL_RE.search(cells[4])
        size_text = TAG_RE.sub('', cells[5]).strip()
        sm = re.search(r'(\d+)', size_text)
        size = int(sm.group(1)) if sm else 0

        nums = [TAG_RE.sub('', c).strip() for c in cells[6:9]]
        am = re.search(r'(\d{9,10})', cells[8])

        items.append({
            'id': int(m.group(1)),
            'title': title,
            **parse_title(title),
            'category': category,
            'forum_id': forum_id,
            'seeders': to_int(nums[0]),
            'leechers': to_int(nums[1]),
            'completed': to_int(nums[2]),
            'size': size,
            'size_text': size_text.split()[-2] + ' ' + size_text.split()[-1] if size_text else '',
            'added': to_int(am.group(1)) if am else 0,
            'url': NNM_BASE + '/forum/viewtopic.php?t=' + m.group(1),
            'download': (NNM_BASE + '/forum/download.php?id=' + dm.group(1)) if dm else '',
        })
    return items


def parse_title(t):
    """«Название / Original (2026) WEBRip ... (сезон 2)» → ru/orig/year/season"""
    ym = re.search(r'\((\d{4})', t)
    year = int(ym.group(1)) if ym else None

    base = re.split(r'[(\[]', t)[0]
    parts = [p.strip(' -') for p in base.split('/') if p.strip(' -')]

    return {
        'ru': parts[0] if parts else '',
        'orig': parts[1] if len(parts) > 1 else '',
        'year': year,
        'season': bool(re.search(r'сезон|серии', t, re.I)),
    }


OPTION_RE = re.compile(r'<option[^>]+value="(\d+)"[^>]*>([^<]*)</option>')


def parse_video_ids(page_html):
    """поддерево видео-разделов по списку категорий страницы трекера"""
    ids = set()
    in_video = False
    for value, text in OPTION_RE.findall(page_html):
        value, text = int(value), html_lib.unescape(text)
        top_level = '|-' not in text
        if top_level:
            in_video = value in VIDEO_ROOTS
        if in_video:
            ids.add(value)
    return ids


def to_int(s):
    m = re.search(r'\d+', s or '')
    return int(m.group(0)) if m else 0


# ---------------- топ

def get_top(cat='video', pages=1):
    cache_key = (cat, pages)
    hit = _cache.get(cache_key)
    if hit and time.time() - hit[0] < TTL:
        return hit[1], True

    pages_html = [fetch_page('/forum/tracker.php?o=10&start=%d' % (PAGE_SIZE * p))
                  for p in range(pages)]

    items = []
    video_ids = None
    if cat == 'video':
        video_ids = parse_video_ids(pages_html[0])

    for page_html in pages_html:
        for item in parse_rows(page_html):
            if video_ids and item['forum_id'] not in video_ids:
                continue
            items.append(item)

    payload = {'source': 'nnmclub',
               'fetched_at': datetime.now(timezone.utc).isoformat(),
               'items': items}
    _cache[cache_key] = (time.time(), payload)
    return payload, False


# ---------------- http

class Handler(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def _send(self, code, obj):
        body = json.dumps(obj, ensure_ascii=False).encode('utf-8')
        self.send_response(code)
        self.send_header('Content-Type', 'application/json; charset=utf-8')
        self.send_header('Content-Length', str(len(body)))
        self.send_header('Access-Control-Allow-Origin', '*')
        self.end_headers()
        self.wfile.write(body)

    def do_OPTIONS(self):
        self.send_response(204)
        self.send_header('Access-Control-Allow-Origin', '*')
        self.send_header('Access-Control-Allow-Methods', 'GET')
        self.send_header('Content-Length', '0')
        self.end_headers()

    def do_GET(self):
        path = self.path.split('?')[0]
        if path == '/healthz':
            return self._send(200, {'ok': True, 'nnm_base': NNM_BASE})
        if path != '/top':
            return self._send(404, {'error': 'not found'})

        query = dict(p.split('=', 1) for p in self.path.split('?')[1].split('&')) \
            if '?' in self.path else {}

        cat = query.get('cat', 'video')
        pages = min(int(query.get('pages', '1')), 3)
        limit = int(query.get('limit', '0')) if query.get('limit') else 0

        try:
            payload, cached = get_top(cat, pages)
        except Exception as e:
            print('fetch error: %r' % e, file=sys.stderr, flush=True)
            return self._send(502, {'error': str(e)})

        out = dict(payload)
        out['cached'] = cached
        if limit > 0:
            out['items'] = out['items'][:limit]
        return self._send(200, out)

    def log_message(self, fmt, *args):
        print('%s %s' % (datetime.now().strftime('%H:%M:%S'), fmt % args), flush=True)


if __name__ == '__main__':
    print('tracker-top on :%d, nnm=%s, ttl=%ds' % (PORT, NNM_BASE, TTL), flush=True)
    ThreadingHTTPServer(('0.0.0.0', PORT), Handler).serve_forever()
