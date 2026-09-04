/// A single 1:1 message between two OCN numbers.
class DmMessage {
  final String id; // server message_id
  final String? clientId; // local correlation for outbound
  final String peer; // canonical full number of the other party
  final String direction; // "in" | "out"
  final String kind; // "text" | "image"
  String text;
  String? imageName;
  String? imageMime;
  String? imagePath; // cached local file once an inbound image is fetched
  String status; // "sending" | "delivered"
  bool read; // inbound only
  final DateTime createdAt;

  DmMessage({
    required this.id,
    this.clientId,
    required this.peer,
    required this.direction,
    required this.kind,
    this.text = '',
    this.imageName,
    this.imageMime,
    this.imagePath,
    this.status = 'delivered',
    this.read = true,
    required this.createdAt,
  });

  bool get isInbound => direction == 'in';
  bool get isImage => kind == 'image';

  DmMessage copyWith({
    String? text,
    String? imagePath,
    String? status,
    bool? read,
  }) => DmMessage(
    id: id,
    clientId: clientId,
    peer: peer,
    direction: direction,
    kind: kind,
    text: text ?? this.text,
    imageName: imageName,
    imageMime: imageMime,
    imagePath: imagePath ?? this.imagePath,
    status: status ?? this.status,
    read: read ?? this.read,
    createdAt: createdAt,
  );

  factory DmMessage.fromJson(Map<String, dynamic> json) => DmMessage(
    id: json['id'] as String,
    clientId: json['client_id'] as String?,
    peer: json['peer'] as String,
    direction: json['direction'] as String,
    kind: json['kind'] as String,
    text: json['text'] as String? ?? '',
    imageName: json['image_name'] as String?,
    imageMime: json['image_mime'] as String?,
    imagePath: json['image_path'] as String?,
    status: json['status'] as String? ?? 'delivered',
    read: json['read'] as bool? ?? true,
    createdAt: DateTime.fromMillisecondsSinceEpoch(json['created_at'] as int? ?? 0),
  );

  Map<String, dynamic> toJson() => {
    'id': id,
    if (clientId != null) 'client_id': clientId,
    'peer': peer,
    'direction': direction,
    'kind': kind,
    'text': text,
    if (imageName != null) 'image_name': imageName,
    if (imageMime != null) 'image_mime': imageMime,
    if (imagePath != null) 'image_path': imagePath,
    'status': status,
    'read': read,
    'created_at': createdAt.millisecondsSinceEpoch,
  };
}

/// One conversation: metadata derived from the most recent message.
class DmThread {
  final String peer;
  final DmMessage lastMessage;
  final int unread;

  DmThread({
    required this.peer,
    required this.lastMessage,
    required this.unread,
  });
}

/// Parsed `dm_event` pushed by the server.
class DmEventInfo {
  final String type; // "new" | "status"
  final String messageId;
  final String? clientId;
  final String? status;
  final String? from;
  final String? fromName;
  final String? kind;
  final String? text;
  final String? imageName;
  final String? imageMime;
  final int? createdAt;

  DmEventInfo({
    required this.type,
    required this.messageId,
    this.clientId,
    this.status,
    this.from,
    this.fromName,
    this.kind,
    this.text,
    this.imageName,
    this.imageMime,
    this.createdAt,
  });

  factory DmEventInfo.fromJson(Map<String, dynamic> json) => DmEventInfo(
    type: json['type'] as String? ?? '',
    messageId: json['message_id'] as String? ?? '',
    clientId: json['client_id'] as String?,
    status: json['status'] as String?,
    from: json['from'] as String?,
    fromName: json['from_name'] as String?,
    kind: json['kind'] as String?,
    text: json['text'] as String?,
    imageName: json['image_name'] as String?,
    imageMime: json['image_mime'] as String?,
    createdAt: json['created_at'] as int?,
  );
}
