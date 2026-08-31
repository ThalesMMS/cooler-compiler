class Message {
  text() : String { "ready" };
};

class Main inherits IO {
  main() : Object {
    let message : Message in
      out_string(message.text())
  };
};
