import 'package:alertme/models.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('Page parses backend JSON', () {
    final p = Page.fromJson({
      'id': 'p1',
      'sender_id': 'a',
      'recipient_id': 'b',
      'message': 'hi',
      'state': 'pushed',
      'attempt': 2,
      'created_at': '2026-11-16T02:00:00Z',
      'expires_at': '2026-11-16T03:00:00Z',
    });
    expect(p.isOpen, isTrue);
    expect(p.attempt, 2);
    expect(p.ackedAt, isNull);
  });

  test('acked page is closed', () {
    final p = Page.fromJson({
      'id': 'p1', 'sender_id': 'a', 'recipient_id': 'b', 'message': 'hi',
      'state': 'acked', 'attempt': 1,
      'created_at': '2026-11-16T02:00:00Z', 'expires_at': '2026-11-16T03:00:00Z',
      'acked_at': '2026-11-16T02:01:00Z', 'ack_note': 'on my way',
    });
    expect(p.isOpen, isFalse);
    expect(p.ackNote, 'on my way');
  });
}
