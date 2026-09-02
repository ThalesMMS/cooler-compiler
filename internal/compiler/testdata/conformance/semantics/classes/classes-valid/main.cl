class Base {
  value : Int <- 1;
  identify(x : Int) : Int { x };
};

class Child inherits Base {
  identify(x : Int) : Int { x + value };
};

class Main inherits IO {
  main() : Object { out_int((new Child).identify(1)) };
};
