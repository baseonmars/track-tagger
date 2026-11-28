"""Atomic cost reservations; uncertain calls remain charged at their ceiling."""
import sqlite3
import time
import uuid
from decimal import Decimal, ROUND_CEILING

MICRO = Decimal(1_000_000)

def micros(dollars):
    return int((Decimal(str(dollars)) * MICRO).to_integral_value(rounding=ROUND_CEILING))

class BudgetExceeded(RuntimeError):
    pass

class Ledger:
    def __init__(self, path, cap=None):
        self.path=str(path)
        requested=micros(cap) if cap is not None else None
        if requested is not None and requested<=0:raise ValueError('budget must be positive')
        with self.connect() as db:
            db.execute('CREATE TABLE IF NOT EXISTS calls (id TEXT PRIMARY KEY, label TEXT, reserved INTEGER NOT NULL, charged INTEGER NOT NULL, status TEXT, created REAL)')
            db.execute('CREATE TABLE IF NOT EXISTS stages (label TEXT PRIMARY KEY, cap INTEGER NOT NULL)')
            db.execute('CREATE TABLE IF NOT EXISTS halt (reason TEXT)')
            db.execute('CREATE TABLE IF NOT EXISTS config (id INTEGER PRIMARY KEY CHECK(id=1), cap INTEGER NOT NULL)')
            db.execute('INSERT OR IGNORE INTO config VALUES (1, ?)', (micros(50),))
            stored=db.execute('SELECT cap FROM config WHERE id=1').fetchone()[0]
            if requested is not None:
                if requested>stored:raise ValueError('budget increase requires explicit audited user authorization')
                db.execute('UPDATE config SET cap=? WHERE id=1',(requested,));stored=requested
            self.cap=stored

    def authorize_total_80(self,authorization,authorized_at):
        """Apply this session's explicit user amendment; ordinary opens cannot raise caps."""
        if not authorization or not isinstance(authorized_at,(int,float)) or authorized_at<=0:raise ValueError('explicit user authorization and timestamp required')
        with self.connect() as db:
            db.execute('BEGIN IMMEDIATE')
            old=db.execute('SELECT cap FROM config WHERE id=1').fetchone()[0]
            if old!=micros(50):raise ValueError('amendment requires unchanged original50 cap')
            db.execute('CREATE TABLE IF NOT EXISTS budget_amendments (id INTEGER PRIMARY KEY,old_cap INTEGER,new_cap INTEGER,authorization TEXT,authorized_at REAL,applied_at REAL)')
            db.execute('INSERT INTO budget_amendments(old_cap,new_cap,authorization,authorized_at,applied_at) VALUES (?,?,?,?,?)',(old,micros(80),authorization,authorized_at,time.time()))
            db.execute('UPDATE config SET cap=? WHERE id=1',(micros(80),))
            self.cap=micros(80)

    def connect(self):
        db = sqlite3.connect(self.path, timeout=30)
        db.execute('PRAGMA busy_timeout=30000')
        return db

    def reserve(self, label, ceiling, stage_cap=None):
        cost = micros(ceiling)
        if cost <= 0:
            raise ValueError('reservation must be positive')
        with self.connect() as db:
            db.execute('BEGIN IMMEDIATE')
            if db.execute('SELECT count(*) FROM halt').fetchone()[0]:
                raise BudgetExceeded('ledger halted after usage overrun')
            cap = db.execute('SELECT cap FROM config WHERE id=1').fetchone()[0]
            used = db.execute('SELECT coalesce(sum(charged),0) FROM calls').fetchone()[0]
            if stage_cap is not None:
                stage_limit=micros(stage_cap)
                db.execute('INSERT OR IGNORE INTO stages VALUES (?,?)',(label,stage_limit))
                db.execute('UPDATE stages SET cap=min(cap,?) WHERE label=?',(stage_limit,label))
                stage_limit=db.execute('SELECT cap FROM stages WHERE label=?',(label,)).fetchone()[0]
                stage_used=db.execute('SELECT coalesce(sum(charged),0) FROM calls WHERE label=?',(label,)).fetchone()[0]
                if stage_used+cost>stage_limit:raise BudgetExceeded('persisted stage budget exhausted')
            if used + cost > cap:
                raise BudgetExceeded('hard experiment budget exhausted')
            key = uuid.uuid4().hex
            db.execute('INSERT INTO calls VALUES (?,?,?,?,?,?)', (key, label, cost, cost, 'reserved', time.time()))
        return key

    def settle(self, key, actual=None):
        with self.connect() as db:
            db.execute('BEGIN IMMEDIATE')
            row = db.execute('SELECT reserved,status FROM calls WHERE id=?', (key,)).fetchone()
            if row is None or row[1] != 'reserved':
                raise ValueError('reservation missing or already settled')
            charge = row[0] if actual is None else micros(actual)
            if charge < 0:
                raise ValueError('negative usage')
            if charge > row[0]:
                db.execute('INSERT INTO halt VALUES (?)', ('usage exceeded reservation',))
                db.execute('UPDATE calls SET charged=?,status=? WHERE id=?', (charge, 'overrun', key))
                db.commit()
                raise BudgetExceeded('usage exceeded reservation; ledger permanently halted')
            db.execute('UPDATE calls SET charged=?,status=? WHERE id=?', (charge, 'uncertain' if actual is None else 'settled', key))

    def allocate_stage(self,label,cap,reason):
        limit=micros(cap)
        if not reason or limit<=0 or limit>max(micros(50),self.cap):raise ValueError('explicit bounded allocation reason required')
        with self.connect() as db:
            db.execute('BEGIN IMMEDIATE')
            db.execute('CREATE TABLE IF NOT EXISTS stage_allocations (id INTEGER PRIMARY KEY, label TEXT, old_cap INTEGER, new_cap INTEGER, reason TEXT, created REAL)')
            old=db.execute('SELECT cap FROM stages WHERE label=?',(label,)).fetchone()
            db.execute('INSERT OR REPLACE INTO stages VALUES (?,?)',(label,limit))
            db.execute('INSERT INTO stage_allocations(label,old_cap,new_cap,reason,created) VALUES (?,?,?,?,?)',(label,old[0] if old else None,limit,reason,time.time()))

    def summary(self):
        with self.connect() as db:
            cap = db.execute('SELECT cap FROM config WHERE id=1').fetchone()[0]
            rows = db.execute('SELECT status,count(*),sum(charged) FROM calls GROUP BY status').fetchall()
        return {'cap_usd': cap / 1e6, 'charged_usd': sum(r[2] for r in rows) / 1e6, 'statuses': {r[0]: r[1] for r in rows}, 'by_status_usd':{r[0]:r[2]/1e6 for r in rows}}
