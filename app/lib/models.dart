/// JSON models matching the backend contract (see BUILD-SPEC.md).
library;

DateTime? _date(dynamic v) => v == null ? null : DateTime.parse(v as String).toLocal();

class Page {
  final String id;
  final String senderId;
  final String recipientId;
  final String message;
  final String state;
  final int attempt;
  final DateTime createdAt;
  final DateTime expiresAt;
  final DateTime? ackedAt;
  final String? ackNote;

  Page.fromJson(Map<String, dynamic> j)
      : id = j['id'] as String,
        senderId = j['sender_id'] as String,
        recipientId = j['recipient_id'] as String,
        message = j['message'] as String? ?? '',
        state = j['state'] as String,
        attempt = (j['attempt'] as num?)?.toInt() ?? 0,
        createdAt = _date(j['created_at'])!,
        expiresAt = _date(j['expires_at'])!,
        ackedAt = _date(j['acked_at']),
        ackNote = j['ack_note'] as String?;

  /// Still nagging the recipient.
  bool get isOpen =>
      state == 'created' || state == 'pushed' || state == 'delivered' || state == 'seen';
}

class Pairing {
  final String userId;
  final String peerId;
  final String peerName;
  final String status;
  final DateTime createdAt;

  Pairing.fromJson(Map<String, dynamic> j)
      : userId = j['user_id'] as String,
        peerId = j['peer_id'] as String,
        peerName = j['peer_name'] as String? ?? '',
        status = j['status'] as String,
        createdAt = _date(j['created_at'])!;

  bool get isActive => status == 'active';
}

class PageEvent {
  final String pageId;
  final String type;
  final String? detail;
  final DateTime at;

  PageEvent.fromJson(Map<String, dynamic> j)
      : pageId = j['page_id'] as String,
        type = j['type'] as String,
        detail = j['detail'] as String?,
        at = _date(j['at'])!;
}

class Invite {
  final String code;
  final DateTime expiresAt;

  Invite.fromJson(Map<String, dynamic> j)
      : code = j['code'] as String,
        expiresAt = _date(j['expires_at'])!;
}
