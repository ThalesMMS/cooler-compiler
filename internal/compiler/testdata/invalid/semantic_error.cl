class Base {
  calculate(x : Int) : Int { x + 1 };
};

class Main inherits Base {
  calculate(x : String) : Int { 0 };

  main() : Int {
    missing.concat("!")
  };
};
