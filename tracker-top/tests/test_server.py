#!/usr/bin/env python3
"""тесты парсера tracker-top на живом снимке страницы топа NNMClub (fixture)"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)) + '/..')
import server  # noqa: E402

FIXTURE = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'nnm_tracker_o10.html')
html = open(FIXTURE, encoding='utf-8').read()

fails = []


def check(name, cond, extra=''):
    print(('ok  ' if cond else 'FAIL') + ' ' + name + ((' — ' + str(extra)) if extra and not cond else ''))
    if not cond:
        fails.append(name)


# --- parse_rows
rows = server.parse_rows(html)
check('rows parsed', len(rows) >= 40, len(rows))
check('sorted by seeders (first >= last)', rows[0]['seeders'] >= rows[-1]['seeders'],
      (rows[0]['seeders'], rows[-1]['seeders']))

top = rows[0]
need_keys = {'id', 'ru', 'orig', 'year', 'season', 'title', 'category', 'forum_id',
             'seeders', 'leechers', 'completed', 'size', 'size_text', 'added', 'url', 'download'}
check('item keys', need_keys <= set(top.keys()), need_keys - set(top.keys()))
check('url viewtopic', top['url'].endswith('viewtopic.php?t=%d' % top['id']), top['url'])
check('download link', 'download.php?id=' in top['download'], top['download'])
check('size_text human', top['size_text'].endswith(('B', 'KB', 'MB', 'GB')), top['size_text'])
check('category non-empty', bool(top['category']), top['category'])

# --- parse_title
cases = [
    ('Джентльмены / The Gentlemen (2026) WEB-DL [H.264/1080p] (сезон 2, серии 1-8)',
     {'ru': 'Джентльмены', 'orig': 'The Gentlemen', 'year': 2026, 'season': True}),
    ('Овчарка (2026) WEBRip [H.264/1080p] (сезон 2, серии 1-12 из 16) (обновляемая)',
     {'ru': 'Овчарка', 'orig': '', 'year': 2026, 'season': True}),
    ('Холоп 3 (2026) WEBRip [H.264/1080p]',
     {'ru': 'Холоп 3', 'orig': '', 'year': 2026, 'season': False}),
    ('Futsutsuka na Akujo de wa Gozaimasu ga: Suuguu Chouso Torikae Byoutan (2026)',
     {'ru': 'Futsutsuka na Akujo de wa Gozaimasu ga: Suuguu Chouso Torikae Byoutan',
      'orig': '', 'year': 2026, 'season': False}),
]
for title, expect in cases:
    got = server.parse_title(title)
    check('parse_title: %s…' % title[:30], got == expect, got)

# --- поддерево видео-категорий
video_ids = server.parse_video_ids(html)
check('video ids parsed', len(video_ids) > 60, len(video_ids))
for fid in (224, 220, 768, 769, 576, 620, 624):
    check('root %d in subtree' % fid, fid in video_ids)
check('soft (503) not in subtree', 503 not in video_ids)
check('books (434) not in subtree', 434 not in video_ids)

video_rows = [r for r in rows if r['forum_id'] in video_ids]
check('video rows subset', 0 < len(video_rows) <= len(rows), (len(video_rows), len(rows)))
check('no soft in video rows', all('Windows' not in r['title'] for r in video_rows))

print('\n%s' % ('ALL OK' if not fails else 'FAILED: %s' % fails))
sys.exit(1 if fails else 0)
