class Counter {
  value : Int <- 1;

  increment() : SELF_TYPE {
    {
      value <- value + 2;
      self;
    }
  };

  current() : Int { value };
};

class Main inherits IO {
  main() : Object {
    let counter : Counter <- new Counter in {
      counter.increment();
      out_int(counter.current());
      out_string("\n");
    }
  };
};
