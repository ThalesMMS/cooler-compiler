class Counter {
  value : Int <- 1;

  increment() : SELF_TYPE {
    {
      value <- value + 1;
      self;
    }
  };

  current() : Int { value };
};

class FastCounter inherits Counter {
  increment() : SELF_TYPE {
    {
      value <- value + 2;
      self;
    }
  };
};

class Main inherits IO {
  main() : Object {
    let counter : Counter <- new FastCounter in {
      counter.increment();
      out_int(counter.current());
      out_string("\n");
    }
  };
};
